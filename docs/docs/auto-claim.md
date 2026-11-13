---
sidebar_position: 7
---

# Auto-Claim Rewards

The validator can claim your staking rewards automatically. Once the unclaimed rewards reach a configured threshold, the validator sends a `claim_rewards` transaction to the staking contract.
This transaction will transfer the rewards to the designated reward address.

To configure this, 5 new flags are made available. Full descriptions are available in [Configuration Options](./configuration-options):
* **`reward-address`** sets the account that sends the `claim_rewards` transactions. It must be either the staker address or the staker's reward address.
* **`reward-priv-key`** (or **`reward-signer-url`**) to sign for the `claim_rewards` transactions.
* **`reward-claim-threshold`** is the amount after which a `claim_rewards` transaction will trigger.
* `reward-claim-max-fee` is the maximum fee you're willing to pay to execute this transaction.

Claim transactions are not time-sensitive, so they are always sent without a tip.

There are [Metrics](./metrics) endpoints made available for this feature.

You can add these new flags to your current config:

```mdx-code-block
import Tabs from "@theme/Tabs";
import TabItem from "@theme/TabItem";
```

<Tabs groupId="config-method">
<TabItem value="flags" label="CLI flags" default>

<Tabs groupId="install">
<TabItem value="docker" label="Docker" default>

```bash
docker run --network host \
  --user "$(id -u):$(id -g)" \
  -v /path/to/config.json:/app/config/config.json \
  nethermind/starknet-staking-v2 \
  --config /app/config/config.json \
  --reward-address "0x789" \
  --reward-signer-url "http://localhost:8081" \
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

</TabItem>
<TabItem value="env" label="Environment variables">

<Tabs groupId="install">
<TabItem value="docker" label="Docker" default>

```bash
docker run --network host \
  --user "$(id -u):$(id -g)" \
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

</TabItem>
<TabItem value="config" label="Config file">

```json
{
  "provider": {
    "http": "http://localhost:6060/rpc/v0_10",
    "ws": "ws://localhost:6061/rpc/v0_10"
  },
  "signer": {
    "operationalAddress": "0x123",
    "url": "http://localhost:8080"
  },
  "reward": {
    "address": "0x789",
    "url": "http://localhost:8081",
    "claimThreshold": 100,
    "claimMaxFee": 1000000000000000000
  }
}
```

<Tabs groupId="install">
<TabItem value="docker" label="Docker" default>

```bash
docker run --network host \
  --user "$(id -u):$(id -g)" \
  -v /path/to/config.json:/app/config/config.json \
  nethermind/starknet-staking-v2 --config /app/config/config.json
```

</TabItem>
<TabItem value="binary" label="Binary">

```bash
./build/validator --config config.json
```

</TabItem>
</Tabs>

</TabItem>
</Tabs>


:::info Delegation pools
Only the staker's own rewards are claimed, which include its commission on the delegated stake. Rewards of the delegation pool members are sent directly to the pool contracts by the staking protocol and claimed by the members themselves, so they are not affected by this feature.
:::

:::tip Reward account balance
The reward account pays the fee of the claim transactions, so it needs enough STRK to cover it. When the feature is enabled, the balance of the reward account is checked against [`--balance-threshold`](./configuration-options#additional-configuration-details) at startup and after every attestation window, just like the operational account, and reported through the [`validator_attestation_reward_balance` and `validator_attestation_reward_below_threshold` metrics](./metrics#available-metrics).
:::

## External Signing

The reward account can be signed for externally using the same [HTTP protocol](./external-signer) as the operational account. The external signer must hold the private key of the reward account, so it is usually a separate signer instance from the one used for attestations. It must be running when the validator starts, since its availability is checked at startup.
