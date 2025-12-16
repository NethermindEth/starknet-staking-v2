package validator

import (
	"strings"
	"time"

	junoUtils "github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
)

// Created a function variable for mocking purposes in tests
var Sleep = time.Sleep

type EventDispatcher[S signerP.Signer] struct {
	// Event channels
	DoAttest      chan types.DoAttest
	PrepareAttest chan types.PrepareAttest
	EndOfWindow   chan struct{}
	// Current epoch attest-related fields
	CurrentAttest AttestTracker
}

func NewEventDispatcher[S signerP.Signer]() EventDispatcher[S] {
	return EventDispatcher[S]{
		CurrentAttest: NewAttestTracker(),
		DoAttest:      make(chan types.DoAttest),
		PrepareAttest: make(chan types.PrepareAttest),
		EndOfWindow:   make(chan struct{}),
	}
}

//nolint:gocyclo // Refactor in another time
func (d *EventDispatcher[S]) Dispatch(
	signer S, balanceThreshold float64, logger *junoUtils.ZapLogger, tracer metrics.Tracer,
) {
	var targetBlockHash types.BlockHash

	for {
		select {
		case attest, ok := <-d.PrepareAttest:
			if !ok {
				return
			}
			if d.CurrentAttest.Status != Iddle {
				logger.Error("receiveing prepare attest info while doing attest")
			}
			if d.CurrentAttest.Transaction.Valid() {
				continue
			}

			targetBlockHash = attest.BlockHash
			logger.Debugf("building attest transaction for blockhash: %s", targetBlockHash.String())
			err := d.CurrentAttest.Transaction.Build(signer, &targetBlockHash)
			if err != nil {
				logger.Errorf("failed to build attest transaction: %s", err.Error())

				continue
			}
			logger.Debug("attest transaction built successfully")

		case attest, ok := <-d.DoAttest:
			if !ok {
				return
			}

			// if the attest event is already being tracked by the tool
			if d.CurrentAttest.Status != Iddle && d.CurrentAttest.Status != Failed {
				// If  status is still not successful, check for it
				if d.CurrentAttest.Status != Successful {
					d.CurrentAttest.UpdateStatus(signer, logger)
				}
				// If status is status is already successful or ongoing, do nothing.
				if d.CurrentAttest.Status == Successful || d.CurrentAttest.Status == Ongoing {
					continue
				}
			}
			d.CurrentAttest.SetStatus(Ongoing)

			// Case when the validator is initiated mid window and didn't have time to prepare
			// or the transaction invoke failed.
			if !d.CurrentAttest.Transaction.Valid() {
				targetBlockHash = attest.BlockHash
				logger.Debugf(
					"building attest transaction (in `do` stage) for blockhash: %s",
					&targetBlockHash,
				)
				err := d.CurrentAttest.Transaction.Build(signer, &targetBlockHash)
				if err != nil {
					logger.Errorf("failed to build attest transaction: %s", err.Error())

					continue
				}
				logger.Debug("attest transaction built successfully")
			} else {
				// Otherwise, the tx was prepared in advance. Update the transaction nonce
				// since it was set some blocks ago
				logger.Debug("updating attest transaction nonce")
				err := d.CurrentAttest.Transaction.UpdateNonce(signer)
				if err != nil {
					logger.Errorf("failed to update transaction nonce: %s", err.Error())

					continue
				}
			}

			logger.Infof("invoking attest; target block hash: %s", targetBlockHash.String())
			resp, err := d.CurrentAttest.Transaction.Invoke(signer)
			if err != nil {
				if strings.Contains(err.Error(), "Attestation is done for this epoch") {
					logger.Infow(
						"attestation is already done for this epoch",
					)
					d.CurrentAttest.SetStatus(Successful)

					continue
				}

				logger.Errorw(
					"failed to attest",
					"error", err.Error(),
				)
				d.CurrentAttest.SetStatus(Failed)

				continue
			}

			logger.Debugw("attest transaction sent", "hash", resp.Hash)
			d.CurrentAttest.Hash = *resp.Hash
			// Record attestation submission in metrics
			tracer.RecordAttestationSubmitted()

		case <-d.EndOfWindow:
			logger.Info("end of window reached")
			if d.CurrentAttest.Status != Successful {
				d.CurrentAttest.UpdateStatus(signer, logger)
			}
			if d.CurrentAttest.Status == Successful {
				logger.Infow(
					"successfully attested to target block",
					"target block hash", targetBlockHash.String(),
				)
				tracer.RecordAttestationConfirmed()
			} else {
				logger.Warnw(
					"failed to attest to target block",
					"target block hash", targetBlockHash.String(),
					"latest attest status", d.CurrentAttest.Status,
				)
				tracer.RecordAttestationFailure()
			}
			// clean slate for the next window
			d.CurrentAttest = NewAttestTracker()
			// check the account balance
			go CheckBalance(signer, balanceThreshold, logger, tracer)
		}
	}
}
