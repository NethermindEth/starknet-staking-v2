package signer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	snGoUtils "github.com/NethermindEth/starknet.go/utils"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const (
	// Selector of the staking contract `claim_rewards` entrypoint, taken from real
	// claim transactions on Sepolia.
	claimRewardsSelectorHex = "0x89c7b1cc4866e9e58d867cb66844bcc9c17cfdabb8ab640b05d2bc53efc556"

	testStakerAddress = "0x7f3f971211da11bbf86527424b9fb7640c0f3fc3c426d3cd1ad833389476a6"
	testRewardAddress = "0x5c85dd30df86ed1f2cfe1806417efb2cae421bffdee8110a74a3d3eb95b28d3"
	testSignerURL     = "http://localhost:1234"
)

func TestRewardSignerAddress(t *testing.T) {
	t.Run("Returns the address of the underlying signer", func(t *testing.T) {
		rewardAddress, err := types.AddressFromString(testRewardAddress)
		require.NoError(t, err)
		mockSigner := mocks.NewMockSigner(gomock.NewController(t))
		mockSigner.EXPECT().Address().Return(&rewardAddress)

		rs := newTestRewardSigner(
			t,
			mockSigner,
			mocks.NewMockRPCProvider(gomock.NewController(t)),
			1,
			1,
		)
		require.Equal(t, &rewardAddress, rs.Address())
	})
}

func TestMakeSignerConfFromRewardConf(t *testing.T) {
	rewardConf := config.Reward{
		ExternalURL:    testSignerURL,
		PrivKey:        "0x123",
		Address:        testRewardAddress,
		ClaimThreshold: 10,
		ClaimMaxFee:    20,
	}

	require.Equal(t, config.Signer{
		ExternalURL:        rewardConf.ExternalURL,
		PrivKey:            rewardConf.PrivKey,
		OperationalAddress: rewardConf.Address,
	}, makeSignerConfFromRewardConf(&rewardConf))
}

func TestNewRewardSigner(t *testing.T) {
	logger := log.NewNopZapLogger()
	stakerAddress, err := types.AddressFromString(testStakerAddress)
	require.NoError(t, err)
	rewardAddress, err := types.AddressFromString(testRewardAddress)
	require.NoError(t, err)
	claimThreshold := uint64(10)
	claimMaxFee := uint64(20)

	// `get_staker_info_v1` response of a staker whose reward address is `testRewardAddress`
	stakerInfo := []string{
		"0x0", testRewardAddress, testStakerAddress, "0x1", "0x1", "0x5", "0x1",
	}

	// Mock provider for creating the internal/external signer and, if `stakerInfo` is
	// not nil, answering the `get_staker_info_v1` call of the reward address check.
	newProvider := func(t *testing.T, stakerInfo []string) *mocks.MockRPCProvider {
		t.Helper()

		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		provider.EXPECT().ChainID(gomock.Any()).Return("SN_SEPOLIA", nil).AnyTimes()
		if stakerInfo != nil {
			provider.EXPECT().
				Call(gomock.Any(), stakerInfoCall(t), rpc.WithBlockTag(rpc.BlockTagLatest)).
				Return(hexFelts(t, stakerInfo...), nil)
		}

		return provider
	}

	newRewardSigner := func(
		t *testing.T, provider rpc.RPCProvider, rewardConf *config.Reward,
	) (RewardSigner, error) {
		t.Helper()

		return NewRewardSigner(
			t.Context(),
			provider,
			logger,
			rewardConf,
			new(config.ContractAddresses),
			&stakerAddress,
			rewardConf.IsBraavos(),
			rewardConf.IsExternal(),
		)
	}

	t.Run("Internal signer", func(t *testing.T) {
		privKey := "0x123"

		rewardConf := config.Reward{
			PrivKey:        privKey,
			Address:        testRewardAddress,
			ClaimThreshold: claimThreshold,
			ClaimMaxFee:    claimMaxFee,
		}

		rs, err := newRewardSigner(t, newProvider(t, stakerInfo), &rewardConf)
		require.NoError(t, err)
		require.NotNil(t, rs)

		rstype, ok := rs.(*rewardSigner)
		require.True(t, ok)

		require.IsType(t, &InternalSigner{}, rstype.signer)
		require.Equal(t, &rewardAddress, rstype.Address())
		require.Equal(t, &stakerAddress, rstype.stakerAddress)
		require.Equal(t, sepoliaContracts(t), rstype.signer.ValidationContracts())
		require.Equal(t, felt.FromUint64[felt.Felt](claimMaxFee), rstype.claimMaxFee)
		require.Equal(t, claimThreshold, rstype.claimThreshold)
	})

	t.Run("External signer", func(t *testing.T) {
		rewardConf := config.Reward{
			ExternalURL:    newTestSignerServer(t),
			Address:        testRewardAddress,
			ClaimThreshold: claimThreshold,
			ClaimMaxFee:    claimMaxFee,
		}
		provider := newProvider(t, stakerInfo)
		// The external signer check signs a claim transaction
		expectBuildClaimTxn(t, provider, felt.NewFromUint64[felt.Felt](5))

		rs, err := newRewardSigner(t, provider, &rewardConf)
		require.NoError(t, err)
		require.NotNil(t, rs)

		rstype, ok := rs.(*rewardSigner)
		require.True(t, ok)

		require.IsType(t, &ExternalSigner{}, rstype.signer)
		require.Equal(t, &rewardAddress, rstype.Address())
		require.Equal(t, &stakerAddress, rstype.stakerAddress)
		require.Equal(t, felt.FromUint64[felt.Felt](claimMaxFee), rstype.claimMaxFee)
		require.Equal(t, claimThreshold, rstype.claimThreshold)
	})

	t.Run("Braavos reward account", func(t *testing.T) {
		internalConf := config.Reward{
			PrivKey:        "0x123",
			Address:        testRewardAddress,
			ClaimThreshold: claimThreshold,
			Braavos:        new(true),
		}
		rs, err := newRewardSigner(t, newProvider(t, stakerInfo), &internalConf)
		require.NoError(t, err)
		internalSigner, ok := rs.(*rewardSigner).signer.(*InternalSigner)
		require.True(t, ok)
		require.True(t, internalSigner.braavos)

		externalConf := config.Reward{
			ExternalURL:    newTestSignerServer(t),
			Address:        testRewardAddress,
			ClaimThreshold: claimThreshold,
			Braavos:        new(true),
		}
		provider := newProvider(t, stakerInfo)
		expectBuildClaimTxn(t, provider, felt.NewFromUint64[felt.Felt](5))
		rs, err = newRewardSigner(t, provider, &externalConf)
		require.NoError(t, err)
		externalSigner, ok := rs.(*rewardSigner).signer.(*ExternalSigner)
		require.True(t, ok)
		require.True(t, externalSigner.braavos)
	})

	t.Run("Staker address as reward account", func(t *testing.T) {
		rewardConf := config.Reward{
			PrivKey:        "0x123",
			Address:        testStakerAddress,
			ClaimThreshold: claimThreshold,
		}

		rs, err := newRewardSigner(t, newProvider(t, stakerInfo), &rewardConf)
		require.NoError(t, err)
		require.Equal(t, &stakerAddress, rs.Address())
	})

	t.Run("Error: invalid reward account can't claim the staker rewards", func(t *testing.T) {
		rewardConf := config.Reward{
			PrivKey:        "0x123",
			Address:        "0x999",
			ClaimThreshold: claimThreshold,
		}

		rs, err := newRewardSigner(t, newProvider(t, stakerInfo), &rewardConf)
		require.ErrorContains(t, err, "is not the real reward address nor the staker address")
		require.Nil(t, rs)
	})

	t.Run("Error: staker not registered in staking contract", func(t *testing.T) {
		rewardConf := config.Reward{
			PrivKey:        "0x123",
			Address:        testRewardAddress,
			ClaimThreshold: claimThreshold,
		}

		rs, err := newRewardSigner(t, newProvider(t, []string{"0x1"}), &rewardConf)
		require.ErrorContains(t, err, "not found by the staking contract")
		require.Nil(t, rs)
	})

	t.Run("Error: external signer is unreachable", func(t *testing.T) {
		// Closed right away, so nothing listens on its URL
		server := httptest.NewServer(http.NotFoundHandler())
		server.Close()

		rewardConf := config.Reward{
			ExternalURL:    server.URL,
			Address:        testRewardAddress,
			ClaimThreshold: claimThreshold,
		}
		provider := newProvider(t, stakerInfo)
		expectBuildClaimTxn(t, provider, felt.NewFromUint64[felt.Felt](5))

		rs, err := newRewardSigner(t, provider, &rewardConf)
		require.ErrorContains(t, err, "external reward signer check")
		require.True(t, rs == nil)
	})

	t.Run("Error: invalid private key", func(t *testing.T) {
		rewardConf := config.Reward{
			PrivKey:        "not a private key",
			Address:        testRewardAddress,
			ClaimThreshold: claimThreshold,
		}

		// Fails before the reward address check, so no `get_staker_info_v1` call
		rs, err := newRewardSigner(t, newProvider(t, nil), &rewardConf)
		require.Error(t, err)
		// Must be an untyped nil, so `rs != nil` checks done by the callers hold
		require.True(t, rs == nil)
	})
}

// TestDecodeGetStakerInfoV1 tests the `decodeGetStakerInfoV1` method of the `rewardSigner` struct.
func TestDecodeGetStakerInfoV1(t *testing.T) {
	const unclaimedWithPool = "0x12345"
	const unclaimedNoPool = "0x67890"

	stakerAddress, err := types.AddressFromString(testStakerAddress)
	require.NoError(t, err)
	rs := &rewardSigner{stakerAddress: &stakerAddress}

	// Vectors captured from the real Sepolia staking contract.
	// 'pool_info' and 'unstake_time' are fields of the Staking contract's `get_staker_info_v1` response.
	validCases := []struct {
		name           string
		resp           []string
		expectedReward string
	}{
		{
			name: "Not 'unstake_time', with 'pool_info'",
			resp: []string{
				"0x0",
				"0x5c85dd30df86ed1f2cfe1806417efb2cae421bffdee8110a74a3d3eb95b28d3",
				"0x7f3f971211da11bbf86527424b9fb7640c0f3fc3c426d3cd1ad833389476a6",
				"0x1",
				"0xde0b6b3a7640000",
				unclaimedWithPool,
				"0x0",
				"0x755e4fbfd6ca9a17e532a0eb3027dd3202957d5bcc2912cbc5a7fb199cc78c6",
				"0x2e247acb00a8f39c2d",
				"0x64",
			},
			expectedReward: unclaimedWithPool,
		},
		{
			name: "Not 'unstake_time', no 'pool_info'",
			resp: []string{
				"0x0",
				"0x21f927e938692ef3a35bcc15739ca7a7bd175233271c8ce85c6dbb81522dc26",
				"0x21f927e938692ef3a35bcc15739ca7a7bd175233271c8ce85c6dbb81522dc26",
				"0x1",
				"0x1ad8e5e30a5fcc54e37",
				unclaimedNoPool,
				"0x1",
			},
			expectedReward: unclaimedNoPool,
		},
		{
			name: "Unstaking ('unstake_time' is Some), no 'pool_info'",
			resp: []string{
				"0x0",
				testRewardAddress,
				testStakerAddress,
				"0x0",
				"0x6720f1a0", // unstake_time.seconds
				"0x1ad8e5e30a5fcc54e37",
				unclaimedNoPool,
				"0x1",
			},
			expectedReward: unclaimedNoPool,
		},
		{
			name: "Unstaking ('unstake_time' is Some), with 'pool_info'",
			resp: []string{
				"0x0",
				testRewardAddress,
				testStakerAddress,
				"0x0",
				"0x6720f1a0", // unstake_time.seconds
				"0xde0b6b3a7640000",
				unclaimedWithPool,
				"0x0",
				"0x755e4fbfd6ca9a17e532a0eb3027dd3202957d5bcc2912cbc5a7fb199cc78c6",
				"0x2e247acb00a8f39c2d",
				"0x64",
			},
			expectedReward: unclaimedWithPool,
		},
		{
			name: "Zero unclaimed rewards",
			resp: []string{
				"0x0", testRewardAddress, testStakerAddress, "0x1", "0x1", "0x0", "0x1",
			},
			expectedReward: "0x0",
		},
	}
	for _, tc := range validCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := hexFelts(t, tc.resp...)
			result, err := rs.decodeGetStakerInfoV1(resp)
			require.NoError(t, err)
			require.Equal(
				t,
				*felt.NewUnsafeFromString[felt.Felt](tc.expectedReward),
				result.unclaimedRewards,
			)
			require.Equal(t, *resp[1], result.rewardAddress)
		})
	}

	errorCases := []struct {
		name string
		resp []string
	}{
		{name: "Nil response", resp: nil},
		{name: "Empty response", resp: []string{}},
		{name: "Not a staker (Option::None)", resp: []string{"0x1"}},
		{name: "Only the option variant", resp: []string{"0x0"}},
		{
			name: "Truncated before unstake_time",
			resp: []string{"0x0", testRewardAddress, testStakerAddress},
		},
		{
			name: "Truncated before amount_own (unstake_time None)",
			resp: []string{"0x0", testRewardAddress, testStakerAddress, "0x1"},
		},
		{
			name: "Truncated before unclaimed_rewards_own (unstake_time None)",
			resp: []string{"0x0", testRewardAddress, testStakerAddress, "0x1", "0x1"},
		},
		{
			name: "Truncated before unstake_time.seconds (unstake_time Some)",
			resp: []string{"0x0", testRewardAddress, testStakerAddress, "0x0"},
		},
		{
			name: "Truncated before unclaimed_rewards_own (unstake_time Some)",
			resp: []string{
				"0x0", testRewardAddress, testStakerAddress, "0x0", "0x6720f1a0", "0x1",
			},
		},
	}
	for _, tc := range errorCases {
		t.Run("Error: "+tc.name, func(t *testing.T) {
			var err error
			require.NotPanics(t, func() {
				_, err = rs.decodeGetStakerInfoV1(hexFelts(t, tc.resp...))
			})
			require.Error(t, err)
		})
	}

	// A Cairo `Option` is serialised with variant 0 (Some) or 1 (None). Any
	// other discriminant is not a valid response.
	invalidVariantCases := []struct {
		name string
		resp []string
	}{
		{
			name: "Invalid Option<StakerInfoV1> variant",
			resp: []string{
				"0x2", testRewardAddress, testStakerAddress, "0x1", "0x1", "0x2", "0x1",
			},
		},
		{
			name: "Invalid Option<Timestamp> (unstake_time) variant",
			resp: []string{
				"0x0", testRewardAddress, testStakerAddress, "0x2", "0x1", "0x2", "0x1",
			},
		},
	}
	for _, tc := range invalidVariantCases {
		t.Run("Error: "+tc.name, func(t *testing.T) {
			_, err := rs.decodeGetStakerInfoV1(hexFelts(t, tc.resp...))
			require.Error(t, err)
		})
	}
}

func TestFetchStakerInfo(t *testing.T) {
	t.Run("Calls get_staker_info_v1 and returns the staker info", func(t *testing.T) {
		mockSigner := mocks.NewMockSigner(gomock.NewController(t))
		mockSigner.EXPECT().ValidationContracts().Return(sepoliaContracts(t)).AnyTimes()

		unclaimed := felt.NewUnsafeFromString[felt.Felt]("0x12345")
		mockSigner.EXPECT().
			Call(stakerInfoCall(t), rpc.WithBlockTag(rpc.BlockTagLatest)).
			Return(stakerInfoResponse(t, unclaimed), nil)

		rs := newTestRewardSigner(
			t,
			mockSigner,
			mocks.NewMockRPCProvider(gomock.NewController(t)),
			1,
			1,
		)
		result, err := rs.fetchStakerInfo()
		require.NoError(t, err)
		require.Equal(t, *unclaimed, result.unclaimedRewards)
		require.Equal(
			t,
			*felt.NewUnsafeFromString[felt.Felt](testRewardAddress),
			result.rewardAddress,
		)
	})

	t.Run("Error: call fails", func(t *testing.T) {
		mockSigner := mocks.NewMockSigner(gomock.NewController(t))
		mockSigner.EXPECT().ValidationContracts().Return(sepoliaContracts(t)).AnyTimes()

		callErr := errors.New("some call error")
		mockSigner.EXPECT().
			Call(stakerInfoCall(t), rpc.WithBlockTag(rpc.BlockTagLatest)).
			Return(nil, callErr)

		rs := newTestRewardSigner(
			t,
			mockSigner,
			mocks.NewMockRPCProvider(gomock.NewController(t)),
			1,
			1,
		)
		_, err := rs.fetchStakerInfo()
		require.ErrorIs(t, err, callErr)
	})
}

func TestBuildClaimRewardTransaction(t *testing.T) {
	rewardAddress, err := types.AddressFromString(testRewardAddress)
	require.NoError(t, err)

	newMockSigner := func(t *testing.T) *mocks.MockSigner {
		t.Helper()

		mockSigner := mocks.NewMockSigner(gomock.NewController(t))
		mockSigner.EXPECT().ValidationContracts().Return(sepoliaContracts(t)).AnyTimes()
		mockSigner.EXPECT().Address().Return(&rewardAddress).AnyTimes()

		return mockSigner
	}

	t.Run("Calldata calls the staking contract `claim_rewards` entrypoint", func(t *testing.T) {
		mockSigner := newMockSigner(t)
		mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil)
		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		expectBuildClaimTxn(t, provider, felt.NewFromUint64[felt.Felt](5))

		rs := newTestRewardSigner(t, mockSigner, provider, 1, 1)
		txn, err := rs.buildClaimRewardTransaction()
		require.NoError(t, err)

		// Sanity check: the hardcoded selector is the `claim_rewards` one
		require.Equal(t,
			felt.NewUnsafeFromString[felt.Felt](claimRewardsSelectorHex),
			snGoUtils.GetSelectorFromNameFelt("claim_rewards"),
		)

		// [num calls, contract, selector, calldata len, staker address]
		expectedCalldata := []*felt.Felt{
			felt.NewUnsafeFromString[felt.Felt]("0x1"),
			sepoliaContracts(t).Staking.Felt(),
			felt.NewUnsafeFromString[felt.Felt](claimRewardsSelectorHex),
			felt.NewUnsafeFromString[felt.Felt]("0x1"),
			felt.NewUnsafeFromString[felt.Felt](testStakerAddress),
		}
		require.Equal(t, expectedCalldata, txn.Calldata)
	})

	t.Run("Transaction fields are populated", func(t *testing.T) {
		mockSigner := newMockSigner(t)
		mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil)
		nonce := felt.NewFromUint64[felt.Felt](42)
		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		// The nonce must be fetched for the reward account
		expectBuildClaimTxn(t, provider, nonce)

		rs := newTestRewardSigner(t, mockSigner, provider, 1, 1)
		txn, err := rs.buildClaimRewardTransaction()
		require.NoError(t, err)

		require.Equal(t, rpc.TransactionTypeInvoke, txn.Type)
		require.Equal(t, rpc.TransactionV3, txn.Version)
		require.Equal(t, rewardAddress.Felt(), txn.SenderAddress)
		require.Equal(t, nonce, txn.Nonce)
		require.Equal(t, rpc.U64("0x0"), txn.Tip)
		require.Equal(t, rpc.DAModeL1, txn.NonceDataMode)
		require.Equal(t, rpc.DAModeL1, txn.FeeMode)
		require.NotNil(t, txn.ResourceBounds)
	})

	t.Run("Error: nonce fails", func(t *testing.T) {
		nonceErr := errors.New("nonce error")
		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		provider.EXPECT().Nonce(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nonceErr)

		rs := newTestRewardSigner(t, newMockSigner(t), provider, 1, 1)
		_, err := rs.buildClaimRewardTransaction()
		require.ErrorIs(t, err, nonceErr)
	})

	t.Run("Error: estimate tip fails", func(t *testing.T) {
		blockErr := errors.New("block error")
		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		provider.EXPECT().
			Nonce(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(felt.NewFromUint64[felt.Felt](5), nil)
		provider.EXPECT().
			BlockWithTxs(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, blockErr)

		rs := newTestRewardSigner(t, newMockSigner(t), provider, 1, 1)
		_, err := rs.buildClaimRewardTransaction()
		require.ErrorIs(t, err, blockErr)
	})

	t.Run("Error: signing fails", func(t *testing.T) {
		signErr := errors.New("sign error")
		mockSigner := newMockSigner(t)
		mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, signErr)
		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		expectBuildClaimTxn(t, provider, felt.NewFromUint64[felt.Felt](5))

		rs := newTestRewardSigner(t, mockSigner, provider, 1, 1)
		_, err := rs.buildClaimRewardTransaction()
		require.ErrorIs(t, err, signErr)
	})
}

func TestPopulateFees(t *testing.T) {
	const claimMaxFee = uint64(1000)

	t.Run("Fee below max fee populates resource bounds", func(t *testing.T) {
		estimate := newFeeEstimation(t, claimMaxFee-1)

		mockSigner := mocks.NewMockSigner(gomock.NewController(t))
		mockSigner.EXPECT().EstimateFee(gomock.Any()).Return(estimate, nil)
		mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil)

		rs := newTestRewardSigner(
			t,
			mockSigner,
			mocks.NewMockRPCProvider(gomock.NewController(t)),
			1,
			claimMaxFee,
		)
		defaultResources := makeDefaultResources()
		txn := rpc.BroadcastInvokeTxnV3{ResourceBounds: &defaultResources}

		overallFee, err := rs.populateFees(&txn)
		require.NoError(t, err)
		require.Equal(t, *estimate.OverallFee, overallFee)
	})

	t.Run("Fee equal to max fee is accepted", func(t *testing.T) {
		estimate := newFeeEstimation(t, claimMaxFee)

		mockSigner := mocks.NewMockSigner(gomock.NewController(t))
		mockSigner.EXPECT().EstimateFee(gomock.Any()).Return(estimate, nil)
		mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil)

		rs := newTestRewardSigner(
			t,
			mockSigner,
			mocks.NewMockRPCProvider(gomock.NewController(t)),
			1,
			claimMaxFee,
		)
		txn := rpc.BroadcastInvokeTxnV3{}

		overallFee, err := rs.populateFees(&txn)
		require.NoError(t, err)
		require.Equal(t, *estimate.OverallFee, overallFee)
	})

	// ClaimRewards logs the returned overall fee when it exceeds the max fee, so the
	// estimated fee must be returned alongside errFeeExceedsClaimMaxFee.
	t.Run("Error: fee above max fee", func(t *testing.T) {
		estimate := newFeeEstimation(t, claimMaxFee+1)

		mockSigner := mocks.NewMockSigner(gomock.NewController(t))
		mockSigner.EXPECT().EstimateFee(gomock.Any()).Return(estimate, nil)
		// SignTransaction must not be called

		rs := newTestRewardSigner(
			t,
			mockSigner,
			mocks.NewMockRPCProvider(gomock.NewController(t)),
			1,
			claimMaxFee,
		)
		defaultResources := makeDefaultResources()
		txn := rpc.BroadcastInvokeTxnV3{ResourceBounds: &defaultResources}

		overallFee, err := rs.populateFees(&txn)
		require.ErrorIs(t, err, errFeeExceedsClaimMaxFee)
		require.Equal(t, *estimate.OverallFee, overallFee)
		// The transaction is left untouched
		require.Equal(t, &defaultResources, txn.ResourceBounds)
	})

	t.Run("Error: estimate fee fails", func(t *testing.T) {
		estimateErr := errors.New("estimate error")

		mockSigner := mocks.NewMockSigner(gomock.NewController(t))
		mockSigner.EXPECT().EstimateFee(gomock.Any()).Return(rpc.FeeEstimation{}, estimateErr)

		rs := newTestRewardSigner(
			t,
			mockSigner,
			mocks.NewMockRPCProvider(gomock.NewController(t)),
			1,
			claimMaxFee,
		)
		txn := rpc.BroadcastInvokeTxnV3{}

		_, err := rs.populateFees(&txn)
		require.ErrorIs(t, err, estimateErr)
		require.NotErrorIs(t, err, errFeeExceedsClaimMaxFee)
	})

	t.Run("Error: signing fails", func(t *testing.T) {
		signErr := errors.New("sign error")

		mockSigner := mocks.NewMockSigner(gomock.NewController(t))
		mockSigner.EXPECT().EstimateFee(gomock.Any()).Return(newFeeEstimation(t, 1), nil)
		mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, signErr)

		rs := newTestRewardSigner(
			t,
			mockSigner,
			mocks.NewMockRPCProvider(gomock.NewController(t)),
			1,
			claimMaxFee,
		)
		txn := rpc.BroadcastInvokeTxnV3{}

		_, err := rs.populateFees(&txn)
		require.ErrorIs(t, err, signErr)
	})
}

// In these tests, the mock provider fails the test on any call that was not expected,
// so a test without provider expectations asserts that no transaction is built or sent.
func TestClaimRewards(t *testing.T) {
	rewardAddress, err := types.AddressFromString(testRewardAddress)
	require.NoError(t, err)
	const claimMaxFee = uint64(1e15)

	// Mock signer which answers `get_staker_info_v1` with `unclaimed` rewards
	newMockSigner := func(t *testing.T, unclaimed *felt.Felt) *mocks.MockSigner {
		t.Helper()

		mockSigner := mocks.NewMockSigner(gomock.NewController(t))
		mockSigner.EXPECT().ValidationContracts().Return(sepoliaContracts(t)).AnyTimes()
		mockSigner.EXPECT().Address().Return(&rewardAddress).AnyTimes()
		mockSigner.EXPECT().
			Call(stakerInfoCall(t), rpc.WithBlockTag(rpc.BlockTagLatest)).
			Return(stakerInfoResponse(t, unclaimed), nil)

		return mockSigner
	}

	t.Run("Unclaimed rewards below threshold: no transaction", func(t *testing.T) {
		// threshold is 100 STRK; unclaimed is 99 FRI, below it under any unit
		mockSigner := newMockSigner(t, felt.NewFromUint64[felt.Felt](99))

		rs := newTestRewardSigner(
			t, mockSigner, mocks.NewMockRPCProvider(gomock.NewController(t)), 100, claimMaxFee,
		)
		rs.ClaimRewards(metrics.NewNoOpMetrics())
	})

	t.Run("Zero unclaimed rewards: no transaction", func(t *testing.T) {
		mockSigner := newMockSigner(t, felt.NewFromUint64[felt.Felt](0))

		rs := newTestRewardSigner(
			t, mockSigner, mocks.NewMockRPCProvider(gomock.NewController(t)), 1, claimMaxFee,
		)
		rs.ClaimRewards(metrics.NewNoOpMetrics())
	})

	// The threshold is configured in STRK while the contract returns FRI.
	t.Run("Threshold is in STRK: 50 STRK unclaimed < 100 STRK threshold", func(t *testing.T) {
		mockSigner := newMockSigner(t, snGoUtils.STRKToFRI(50))

		rs := newTestRewardSigner(
			t, mockSigner, mocks.NewMockRPCProvider(gomock.NewController(t)), 100, claimMaxFee,
		)
		rs.ClaimRewards(metrics.NewNoOpMetrics())
	})

	t.Run("Error: get_staker_info_v1 call fails: no transaction", func(t *testing.T) {
		mockSigner := mocks.NewMockSigner(gomock.NewController(t))
		mockSigner.EXPECT().ValidationContracts().Return(sepoliaContracts(t)).AnyTimes()
		mockSigner.EXPECT().
			Call(gomock.Any(), gomock.Any()).
			Return(nil, errors.New("call error"))

		rs := newTestRewardSigner(
			t, mockSigner, mocks.NewMockRPCProvider(gomock.NewController(t)), 1, claimMaxFee,
		)
		rs.ClaimRewards(metrics.NewNoOpMetrics())
	})

	t.Run("Error: not a staker: no transaction", func(t *testing.T) {
		mockSigner := mocks.NewMockSigner(gomock.NewController(t))
		mockSigner.EXPECT().ValidationContracts().Return(sepoliaContracts(t)).AnyTimes()
		mockSigner.EXPECT().
			Call(gomock.Any(), gomock.Any()).
			Return([]*felt.Felt{&felt.One}, nil)

		rs := newTestRewardSigner(
			t, mockSigner, mocks.NewMockRPCProvider(gomock.NewController(t)), 1, claimMaxFee,
		)
		rs.ClaimRewards(metrics.NewNoOpMetrics())
	})

	t.Run("Error: building the transaction fails: no transaction sent", func(t *testing.T) {
		mockSigner := newMockSigner(t, snGoUtils.STRKToFRI(200))
		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		provider.EXPECT().
			Nonce(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, errors.New("nonce error"))

		rs := newTestRewardSigner(t, mockSigner, provider, 1, claimMaxFee)
		rs.ClaimRewards(metrics.NewNoOpMetrics())
	})

	t.Run("Estimated fee above max fee: no transaction sent", func(t *testing.T) {
		mockSigner := newMockSigner(t, snGoUtils.STRKToFRI(200))
		mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil)
		mockSigner.EXPECT().
			EstimateFee(gomock.Any()).
			Return(newFeeEstimation(t, claimMaxFee+1), nil)
		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		expectBuildClaimTxn(t, provider, felt.NewFromUint64[felt.Felt](5))

		rs := newTestRewardSigner(t, mockSigner, provider, 1, claimMaxFee)
		rs.ClaimRewards(metrics.NewNoOpMetrics())
	})

	t.Run("Error: estimate fee fails: no transaction sent", func(t *testing.T) {
		mockSigner := newMockSigner(t, snGoUtils.STRKToFRI(200))
		mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil)
		mockSigner.EXPECT().
			EstimateFee(gomock.Any()).
			Return(rpc.FeeEstimation{}, errors.New("estimate error"))
		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		expectBuildClaimTxn(t, provider, felt.NewFromUint64[felt.Felt](5))

		rs := newTestRewardSigner(t, mockSigner, provider, 1, claimMaxFee)
		rs.ClaimRewards(metrics.NewNoOpMetrics())
	})

	t.Run("Error: sending the transaction fails", func(t *testing.T) {
		mockSigner := newMockSigner(t, snGoUtils.STRKToFRI(200))
		mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil).Times(2)
		mockSigner.EXPECT().EstimateFee(gomock.Any()).Return(newFeeEstimation(t, 1), nil)
		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		expectBuildClaimTxn(t, provider, felt.NewFromUint64[felt.Felt](5))
		expectAddInvoke(t, provider, errors.New("add invoke error"))

		rs := newTestRewardSigner(t, mockSigner, provider, 1, claimMaxFee)
		rs.ClaimRewards(metrics.NewNoOpMetrics())
	})

	t.Run("Unclaimed rewards above threshold: claim is sent", func(t *testing.T) {
		estimate := newFeeEstimation(t, claimMaxFee-1)
		nonce := felt.NewFromUint64[felt.Felt](5)

		mockSigner := newMockSigner(t, snGoUtils.STRKToFRI(200))
		// Signed once to estimate the fee, and again once the resource bounds are set
		mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil).Times(2)
		mockSigner.EXPECT().EstimateFee(gomock.Any()).Return(estimate, nil)
		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		expectBuildClaimTxn(t, provider, nonce)
		sent := expectAddInvoke(t, provider, nil)

		rs := newTestRewardSigner(t, mockSigner, provider, 100, claimMaxFee)
		rs.ClaimRewards(metrics.NewNoOpMetrics())

		require.Equal(t, rewardAddress.Felt(), sent.SenderAddress)
		require.Equal(t,
			snGoUtils.FeeEstToResBoundsMap(estimate, constants.FeeEstimationMultiplier),
			sent.ResourceBounds,
		)
		require.Equal(t, rpc.TransactionV3, sent.Version)
		require.Equal(t, nonce, sent.Nonce)
	})

	t.Run("Unclaimed rewards equal to threshold: claim is sent", func(t *testing.T) {
		mockSigner := newMockSigner(t, snGoUtils.STRKToFRI(10))
		mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil).Times(2)
		mockSigner.EXPECT().EstimateFee(gomock.Any()).Return(newFeeEstimation(t, 1), nil)
		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		expectBuildClaimTxn(t, provider, felt.NewFromUint64[felt.Felt](5))
		expectAddInvoke(t, provider, nil)

		rs := newTestRewardSigner(t, mockSigner, provider, 10, claimMaxFee)
		rs.ClaimRewards(metrics.NewNoOpMetrics())
	})
}

// Records the reward claim related calls, ignoring the rest
type claimTracer struct {
	metrics.NoOpMetrics

	unclaimed      *float64
	submitted      int
	failures       int
	maxFeeExceeded int
}

func (c *claimTracer) UpdateRewardUnclaimed(amount float64) { c.unclaimed = &amount }

func (c *claimTracer) RecordRewardClaimSubmitted() { c.submitted++ }

func (c *claimTracer) RecordRewardClaimFailure() { c.failures++ }

func (c *claimTracer) RecordRewardClaimMaxFeeExceeded() { c.maxFeeExceeded++ }

// Checks that ClaimRewards records each claim metric
func TestClaimRewardsMetrics(t *testing.T) {
	rewardAddress, err := types.AddressFromString(testRewardAddress)
	require.NoError(t, err)
	unclaimed := snGoUtils.STRKToFRI(200)
	unclaimedStrk := snGoUtils.FRIToSTRK(unclaimed)
	const claimMaxFee = uint64(1e15)

	// Mocks of a reward signer whose staker has `unclaimed` rewards
	newMocks := func(t *testing.T) (*mocks.MockSigner, *mocks.MockRPCProvider) {
		t.Helper()

		mockSigner := mocks.NewMockSigner(gomock.NewController(t))
		mockSigner.EXPECT().ValidationContracts().Return(sepoliaContracts(t)).AnyTimes()
		mockSigner.EXPECT().Address().Return(&rewardAddress).AnyTimes()
		mockSigner.EXPECT().
			Call(stakerInfoCall(t), rpc.WithBlockTag(rpc.BlockTagLatest)).
			Return(stakerInfoResponse(t, unclaimed), nil)

		return mockSigner, mocks.NewMockRPCProvider(gomock.NewController(t))
	}

	t.Run("Unclaimed rewards", func(t *testing.T) {
		mockSigner, provider := newMocks(t)

		// Threshold above the unclaimed rewards, so no claim is attempted
		rs := newTestRewardSigner(t, mockSigner, provider, 1000, claimMaxFee)
		tracer := &claimTracer{}
		rs.ClaimRewards(tracer)

		require.Equal(t, &claimTracer{unclaimed: &unclaimedStrk}, tracer)
	})

	t.Run("Claim failure", func(t *testing.T) {
		mockSigner, provider := newMocks(t)
		provider.EXPECT().
			Nonce(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, errors.New("nonce error"))

		rs := newTestRewardSigner(t, mockSigner, provider, 1, claimMaxFee)
		tracer := &claimTracer{}
		rs.ClaimRewards(tracer)

		require.Equal(t, &claimTracer{unclaimed: &unclaimedStrk, failures: 1}, tracer)
	})

	t.Run("Max fee exceeded", func(t *testing.T) {
		mockSigner, provider := newMocks(t)
		mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil)
		mockSigner.EXPECT().
			EstimateFee(gomock.Any()).
			Return(newFeeEstimation(t, claimMaxFee+1), nil)
		expectBuildClaimTxn(t, provider, felt.NewFromUint64[felt.Felt](5))

		rs := newTestRewardSigner(t, mockSigner, provider, 1, claimMaxFee)
		tracer := &claimTracer{}
		rs.ClaimRewards(tracer)

		require.Equal(t,
			&claimTracer{unclaimed: &unclaimedStrk, maxFeeExceeded: 1},
			tracer,
		)
	})

	t.Run("Claim submitted", func(t *testing.T) {
		mockSigner, provider := newMocks(t)
		mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil).Times(2)
		mockSigner.EXPECT().EstimateFee(gomock.Any()).Return(newFeeEstimation(t, 1), nil)
		expectBuildClaimTxn(t, provider, felt.NewFromUint64[felt.Felt](5))
		expectAddInvoke(t, provider, nil)

		rs := newTestRewardSigner(t, mockSigner, provider, 1, claimMaxFee)
		tracer := &claimTracer{}
		rs.ClaimRewards(tracer)

		require.Equal(t, &claimTracer{unclaimed: &unclaimedStrk, submitted: 1}, tracer)
	})
}

func TestFriToStrkString(t *testing.T) {
	testCases := []struct {
		fri      string
		expected string
	}{
		{fri: "0", expected: "0.00"},
		{fri: "1", expected: "0.00"},
		// Truncated, not rounded to 1.00
		{fri: "999999999999999999", expected: "0.99"},
		{fri: "1000000000000000000", expected: "1.00"},
		{fri: "1500000000000000000", expected: "1.50"},
		// Truncated, not rounded to 0.83
		{fri: "828251213175784188", expected: "0.82"},
		{fri: "123456000000000000000000", expected: "123456.00"},
	}

	for _, tc := range testCases {
		t.Run(tc.fri+" FRI", func(t *testing.T) {
			fri, err := felt.NewFromString[felt.Felt](tc.fri)
			require.NoError(t, err)
			require.Equal(t, tc.expected, friToStrkString(fri))
		})
	}
}

func newTestRewardSigner(
	t *testing.T,
	signer Signer,
	provider rpc.RPCProvider,
	claimThresholdStrk uint64,
	claimMaxFeeFri uint64,
) *rewardSigner {
	t.Helper()

	stakerAddress, err := types.AddressFromString(testStakerAddress)
	require.NoError(t, err)

	return &rewardSigner{
		ctx:            t.Context(),
		logger:         log.NewNopZapLogger(),
		signer:         signer,
		provider:       provider,
		stakerAddress:  &stakerAddress,
		claimMaxFee:    felt.FromUint64[felt.Felt](claimMaxFeeFri),
		claimThreshold: claimThresholdStrk,
	}
}

// newTestSignerServer starts an external signer answering any request with a dummy
// signature, and returns its URL
func newTestSignerServer(t *testing.T) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(`{"signature": ["0x123", "0x456"]}`))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	return server.URL
}

func hexFelts(t *testing.T, values ...string) []*felt.Felt {
	t.Helper()

	result := make([]*felt.Felt, len(values))
	for i, v := range values {
		result[i] = felt.NewUnsafeFromString[felt.Felt](v)
	}

	return result
}

func sepoliaContracts(t *testing.T) *types.ValidationContracts {
	t.Helper()

	contracts, err := types.ValidationContractsFromAddresses(
		new(config.ContractAddresses).SetDefaults("SN_SEPOLIA"),
	)
	require.NoError(t, err)

	return &contracts
}

func stakerInfoCall(t *testing.T) rpc.FunctionCall {
	t.Helper()

	return rpc.FunctionCall{
		ContractAddress:    sepoliaContracts(t).Staking.Felt(),
		EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("get_staker_info_v1"),
		Calldata:           []*felt.Felt{felt.NewUnsafeFromString[felt.Felt](testStakerAddress)},
	}
}

// stakerInfoResponse builds a `get_staker_info_v1` response for a staker which
// is not unstaking and has no pool, with the given unclaimed rewards.
func stakerInfoResponse(t *testing.T, unclaimed *felt.Felt) []*felt.Felt {
	t.Helper()

	resp := hexFelts(t,
		"0x0",
		testRewardAddress,
		testStakerAddress,
		"0x1",
		"0x1ad8e5e30a5fcc54e37",
	)
	resp = append(resp, unclaimed, felt.NewUnsafeFromString[felt.Felt]("0x1"))

	return resp
}

func newFeeEstimation(t *testing.T, overallFee uint64) rpc.FeeEstimation {
	t.Helper()

	return rpc.FeeEstimation{
		FeeEstimationCommon: rpc.FeeEstimationCommon{
			L1GasConsumed:     felt.NewUnsafeFromString[felt.Felt]("0x10"),
			L1GasPrice:        felt.NewUnsafeFromString[felt.Felt]("0x20"),
			L2GasConsumed:     felt.NewUnsafeFromString[felt.Felt]("0x30"),
			L2GasPrice:        felt.NewUnsafeFromString[felt.Felt]("0x40"),
			L1DataGasConsumed: felt.NewUnsafeFromString[felt.Felt]("0x50"),
			L1DataGasPrice:    felt.NewUnsafeFromString[felt.Felt]("0x60"),
			OverallFee:        felt.NewFromUint64[felt.Felt](overallFee),
		},
		Unit: rpc.FriUnit,
	}
}

// expectBuildClaimTxn expects the provider calls performed when building a claim
// transaction: the reward account nonce, and the latest block used to estimate the
// tip (an empty block gives a tip of 0x0).
func expectBuildClaimTxn(t *testing.T, provider *mocks.MockRPCProvider, nonce *felt.Felt) {
	t.Helper()

	rewardAddress, err := types.AddressFromString(testRewardAddress)
	require.NoError(t, err)
	provider.EXPECT().
		Nonce(gomock.Any(), rpc.WithBlockTag(rpc.BlockTagPreConfirmed), rewardAddress.Felt()).
		Return(nonce, nil)
	provider.EXPECT().
		BlockWithTxs(gomock.Any(), rpc.WithBlockTag(rpc.BlockTagLatest), gomock.Nil()).
		Return(&rpc.Block{}, nil)
}

// expectAddInvoke expects a single transaction to be sent, answered with `err`.
// The returned pointer holds the sent transaction once it is sent.
func expectAddInvoke(
	t *testing.T, provider *mocks.MockRPCProvider, err error,
) *rpc.BroadcastInvokeTxnV3 {
	t.Helper()

	sent := new(rpc.BroadcastInvokeTxnV3)
	provider.EXPECT().
		AddInvokeTransaction(gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			_ context.Context, txn *rpc.BroadcastInvokeTxnV3,
		) (rpc.AddInvokeTransactionResponse, error) {
			*sent = *txn
			if err != nil {
				return rpc.AddInvokeTransactionResponse{}, err
			}

			return rpc.AddInvokeTransactionResponse{
				Hash: felt.NewUnsafeFromString[felt.Felt]("0xabc"),
			}, nil
		})

	return sent
}
