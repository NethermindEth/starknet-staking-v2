---
sidebar_position: 4
hide_table_of_contents: true
---

# Configuration Options

This page provides a complete reference of all configuration options available in the Starknet Staking v2 validator.

In addition to the core configuration described in the [Setup](./setup) page, the tool allows for other non-essential customization. You can see all available options by using the `--help` flag:

```bash
# Standalone binary
./build/validator --help

```

## Complete Options Reference

Flags take priority over environment variables, which take priority over the config file. The Config File column is the key path in the JSON file: `reward.claimMaxFee` is `{"reward": {"claimMaxFee": ...}}`. An option with `-` in both columns can only be set as a flag.

```mdx-code-block
import ConfigOptions from "./_config-options.md";
import ExpandableTable from "@site/src/components/ExpandableTable";

<ExpandableTable>
<ConfigOptions />
</ExpandableTable>
```

## Additional Configuration Details

1. **Contract Addresses**: `--staking-contract-address` and `--attest-contract-address` set the staking and attestation contract addresses. On Mainnet and Sepolia they default to the official contracts. On any other network both are required.

2. **Max Retries**: `--max-retries` sets how many times the tool retries getting the information it needs to attest. It can be any positive integer or _"infinite"_, the default, so the tool never stops retrying.

3. **Balance Threshold**: `--balance-threshold` represents the balance amount you want your signer account to be above of. If the auto-claim reward feature is configured, the same threshold also applies to the reward account. Checks are performed after every attestation window ends and if a balance is below the specified amount a warning is emitted. Defaults to 100 STRK.

4. **Log Level**: `--log-level` sets the tool logging level. Defaults to `info`.

5. **Auto-Claim Rewards**: the `--reward-*` options configure the optional automatic claiming of the staking rewards. If any of them is set, `--reward-address`, `--reward-claim-threshold` and exactly one of `--reward-priv-key` or `--reward-signer-url` are required. See [Auto-Claim Rewards](./auto-claim) for the details.

6. **Max Tip**: `--max-tip` caps the tip of the attestation transactions, in FRI per L2 gas unit. The tip is estimated as the average tip of the transactions in the latest block, multiplied by 1.5. A single transaction with an unusually high tip can therefore make the estimate spike, so setting a max tip limits what an attestation can pay. When the estimated tip is above the max tip, the max tip is used instead and a warning is logged. Set it to 0 to never pay a tip. If not set, no limit is applied.

:::warning Low max tip
The tip affects how fast a transaction is included in a block. With no tip, or a max tip well below what other transactions are paying, an attestation may take longer to be included, especially when the network is busy. If it isn't included before the attestation window ends, the attestation is missed.
:::
