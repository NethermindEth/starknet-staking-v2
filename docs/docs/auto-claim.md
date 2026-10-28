---
sidebar_position: 7
---

# Auto-Claim Rewards

The validator can optionally claim your staking rewards automatically. Once the unclaimed rewards of the staker reach a configured threshold, the validator sends a `claim_rewards` transaction to the staking contract, which transfers them to the staker's reward address.

The feature is disabled by default. It is enabled as soon as any reward option is set.

## How It Works

1. **At startup**, the validator fetches the staker address linked to your operational address and checks that the configured reward account is allowed to claim its rewards. The staking contract only accepts claims sent by the **staker address** or by the staker's **reward address**. Any other account makes the validator fail to start. When using an [external signer](#external-signing), the validator also asks it to sign a claim transaction, without sending it, and fails to start if the signer can't be reached.
2. **After every attestation window ends**, the validator reads the staker's unclaimed rewards from the staking contract. If they reach the claim threshold, it:
   1. builds a `claim_rewards` transaction signed by the reward account,
   2. estimates its fee and skips the claim with a warning if the fee is higher than the configured max fee,
   3. sends the transaction.

Every claim error is logged, and none of them stops the validator: attestations keep running normally. Claims, failures and claims skipped due to the max fee are also reported through the [`validator_attestation_reward_*` metrics](./metrics#available-metrics), along with the staker's unclaimed rewards.

:::info Delegation pools
Only the staker's own rewards are claimed, which include its commission on the delegated stake. Rewards of the delegation pool members are sent directly to the pool contracts by the staking protocol and claimed by the members themselves, so they are not affected by this feature.
:::

:::tip Reward account balance
The reward account pays the fee of the claim transactions, so it needs enough STRK to cover it. When the feature is enabled, the balance of the reward account is checked against [`--balance-threshold`](./configuration-options#additional-configuration-details) at startup and after every attestation window, just like the operational account, and reported through the [`validator_attestation_reward_balance` and `validator_attestation_reward_below_threshold` metrics](./metrics#available-metrics).
:::

## Configuration

| Option | Environment Variable | Config File | Default | Description |
|--------|---------------------|-------------|---------|-------------|
| `--reward-address` | `REWARD_ADDRESS` | `reward.address` | - | Address of the reward account, which signs and sends the claim transactions. Must be either the staker address or the staker's reward address |
| `--reward-priv-key` | `REWARD_PRIVATE_KEY` | `reward.privateKey` | - | Private key of the reward account, for internal signing |
| `--reward-signer-url` | `REWARD_EXTERNAL_URL` | `reward.url` | - | URL of an external signer holding the reward account key, for external signing |
| `--reward-claim-threshold` | `REWARD_CLAIM_THRESHOLD` | `reward.claimThreshold` | - | Amount of unclaimed rewards, in STRK (whole units), that triggers a claim |
| `--reward-claim-max-fee` | `REWARD_CLAIM_MAX_FEE` | `reward.claimMaxFee` | Unlimited | Max fee, in FRI, to pay for a claim transaction. If not set, no limit is applied (max uint64, ~18.4 STRK) |
| `--reward-braavos-account` | `REWARD_BRAAVOS_ACCOUNT` | `reward.braavos` | `false` | Set it if the reward account is a Braavos account. Independent of `--braavos-account`, which only applies to the operational account |

If any of these options is set, even `--reward-braavos-account=false`, the reward configuration must be complete:

- `--reward-address` is required.
- Exactly one signing method is required: either `--reward-priv-key` or `--reward-signer-url`. As for the operational signer, setting both is an error.
- `--reward-claim-threshold` is required and must be greater than zero.

The options follow the same [precedence](./configuration) as the rest of the configuration: command line flags, then environment variables, then the configuration file.

### Examples

```mdx-code-block
import Tabs from "@theme/Tabs";
import TabItem from "@theme/TabItem";
```

With command line flags:

<Tabs groupId="install">
<TabItem value="docker" label="Docker" default>

```bash
docker run \
  -v /path/to/config.json:/app/config/config.json \
  nethermind/starknet-staking-v2 \
  --config /app/config/config.json \
  --reward-address "0x789" \
  --reward-signer-url "http://host.docker.internal:8081" \
  --reward-claim-threshold 100 \
  --reward-claim-max-fee 1000000000000000000
```

</TabItem>
<TabItem value="binary" label="Binary">

```bash
./build/validator \
  --config config.json \
  --reward-address "0x789" \
  --reward-signer-url "http://localhost:8081" \
  --reward-claim-threshold 100 \
  --reward-claim-max-fee 1000000000000000000
```

</TabItem>
</Tabs>

With environment variables:

<Tabs groupId="install">
<TabItem value="docker" label="Docker" default>

```bash
docker run \
  -v /path/to/config.json:/app/config/config.json \
  -e REWARD_ADDRESS="0x789" \
  -e REWARD_PRIVATE_KEY="0xabc" \
  -e REWARD_CLAIM_THRESHOLD=100 \
  nethermind/starknet-staking-v2 --config /app/config/config.json
```

</TabItem>
<TabItem value="binary" label="Binary">

```bash
REWARD_ADDRESS="0x789" \
REWARD_PRIVATE_KEY="0xabc" \
REWARD_CLAIM_THRESHOLD=100 \
./build/validator --config config.json
```

</TabItem>
</Tabs>

With a configuration file:

```json
{
  "provider": {
    "http": "http://localhost:6060/v0_10",
    "ws": "ws://localhost:6061/v0_10"
  },
  "signer": {
    "operationalAddress": "0x123",
    "url": "http://localhost:8080"
  },
  "reward": {
    "address": "0x789",
    "url": "http://localhost:8081",
    "claimThreshold": 100,
    "claimMaxFee": 1000000000000000000,
    "braavos": false
  }
}
```

## External Signing

The reward account can be signed for externally using the same [HTTP protocol](./external-signer) as the operational account. The external signer must hold the private key of the reward account, so it is usually a separate signer instance from the one used for attestations. It must be running when the validator starts, since its availability is checked at startup.
