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

| Option | Environment Variable | Config File | Default | Description |
|--------|---------------------|-------------|---------|-------------|
| `--provider-http` | `PROVIDER_HTTP_URL` | `provider.http` | - | HTTP endpoint for JSON-RPC calls |
| `--provider-ws` | `PROVIDER_WS_URL` | `provider.ws` | - | WebSocket endpoint for real-time updates |
| `--signer-op-address` | `SIGNER_OPERATIONAL_ADDRESS` | `signer.operationalAddress` | - | Your validator's operational address |
| `--signer-priv-key` | `SIGNER_PRIVATE_KEY` | `signer.privateKey` | - | Private key for internal signing |
| `--signer-url` | `SIGNER_EXTERNAL_URL` | `signer.url` | - | URL for external signing service |
| `--config` | - | - | - | Path to JSON configuration file |
| `--staking-contract-address` | - | - | Auto-detected | Custom staking contract address |
| `--attest-contract-address` | - | - | Auto-detected | Custom attestation contract address |
| `--max-tries` | - | - | `10` | Maximum attempts to get attestation info (or "infinite") |
| `--balance-threshold` | - | - | `100` | riggers a warning if it detects the signer account (i.e. operational address) stark balance below the specified threshold. One stark equals 1e18 |
| `--log-level` | - | - | `info` | Set logging level (trace, debug, info, warn, error) |
| `--metrics` | - | - | `false` | Enable metrics server |
| `--metrics-host` | - | - | `localhost` | Metrics server host |
| `--metrics-port` | - | - | `9090` | Metrics server port |
| `--braavos-account` | - | - | `false` | Enable Braavos account support (experimental) |

## Additional Configuration Details

1. **Contract Addresses**: Using specific staking and attestation contract addresses through the `--staking-contract-address` and `--attest-contract-address` flags respectively. If no values are provided, sensible defaults are provided based on the network id.

2. **Max Tries**: `--max-tries` allows you to set how many attempts the tool does to get attestation information. It can be set to any positive number or to _"infinite"_ if you want the tool to never stop execution. Defaults to 10.

4. **Balance Threshold**: `--balance-threshold` represents the balance amount you want your signer account to be above of. Checks are performed after every attestation window ends and if the balance is below the specified amount a warning is emitted. Defaults to 100 STRK.

5. **Log Level**: `--log-level` set's the tool logging level. Default to `info`.

6. **Braavos Account**: `--braavos-account` changes the transaction version format from `0x3` to `1<<128 + 0x3` required by Braavos accounts. _Note that this is still an experimental feature_.
