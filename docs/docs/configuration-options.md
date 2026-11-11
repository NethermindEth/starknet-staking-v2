---
sidebar_position: 4
---

# Configuration Options

This page provides a complete reference of all configuration options available in the Starknet Staking v2 validator.

In addition to the core configuration described in the [Configuration](./configuration) page, the tool allows for other non-essential customization. You can see all available options by using the `--help` flag:

```bash
# Standalone binary
./build/validator --help

```

## Complete Options Reference

Flags take priority over environment variables, which take priority over the config file. The Config File column is the key path in the JSON file: `reward.claimMaxFee` is `{"reward": {"claimMaxFee": ...}}`. An option with `-` in both columns can only be set as a flag.

```mdx-code-block
import ConfigOptions from "./_config-options.md";

<ConfigOptions />
```

## Additional Configuration Details

1. **Contract Addresses**: `--staking-contract-address` and `--attest-contract-address` set the staking and attestation contract addresses. On Mainnet and Sepolia they default to the official contracts. On any other network both are required.

2. **Max Retries**: `--max-retries` sets how many times the tool retries getting the information it needs to attest. It can be any positive integer or _"infinite"_, the default, so the tool never stops retrying.

3. **Balance Threshold**: `--balance-threshold` represents the balance amount you want your signer account to be above of. If the auto-claim reward feature is configured, the same threshold also applies to the reward account. Checks are performed after every attestation window ends and if a balance is below the specified amount a warning is emitted. Defaults to 100 STRK.

4. **Log Level**: `--log-level` sets the tool logging level. Defaults to `info`.

5. **Braavos Account**: `--braavos-account` makes fee estimation use the query-bit transaction version `1<<128 + 0x3` that Braavos accounts require, with both internal and external signing. _Note that this is still an experimental feature_. It only applies to the operational account: use `--reward-braavos-account` for the reward account.

6. **Auto-Claim Rewards**: the `--reward-*` options configure the optional automatic claiming of the staking rewards. If any of them is set, `--reward-address`, `--reward-claim-threshold` and exactly one of `--reward-priv-key` or `--reward-signer-url` are required. See [Auto-Claim Rewards](./auto-claim) for the details.
