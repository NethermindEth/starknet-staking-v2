---
sidebar_position: 3
---

# Configuration

The Starknet Staking v2 validator can be configured using several methods, with the following order of precedence:

1. **[Command line parameters](#command-line-parameters)**
2. **[Environment variables](#environment-variables)**
3. **[Configuration file](#configuration-file)**

:::tip Protocol Context
For detailed information about validator roles, address types, and security practices in Starknet's staking protocol, see the [official Starknet staking documentation](https://docs.starknet.io/architecture-and-concepts/staking/).
:::

## Command Line Parameters

The validator can be configured directly on the command line, e.g:

```bash
docker run nethermind/starknet-staking-v2 \
    --provider-http "http://localhost:6060/rpc/v0_9" \
    --provider-ws "ws://localhost:6061/ws/v0_9" \
    --signer-op-address "0x123" \
    --signer-url "http://localhost:8080"
```

:::tip
Command line parameters override environment variables and configuration file.
:::

## Environment Variables

The validator can be configured through environment variables using specific variable names:

```bash
PROVIDER_HTTP_URL="http://localhost:6060/v0_9" \
PROVIDER_WS_URL="ws://localhost:6061/v0_9" \
SIGNER_OPERATIONAL_ADDRESS="0x123" \
SIGNER_PRIVATE_KEY="0x456" \
./build/validator
```

### Using a .env File

Create a `.env` file with your configuration:

```bash title=".env"
PROVIDER_HTTP_URL="http://localhost:6060/v0_9"
PROVIDER_WS_URL="ws://localhost:6061/v0_9"

SIGNER_OPERATIONAL_ADDRESS="0x123"
SIGNER_EXTERNAL_URL="http://localhost:8080"
```

Then source the environment variables and run the validator:

```bash
source .env
./build/validator
```

### Docker with Environment Variables

When using Docker, set environment variables using the `-e` option:

```bash
docker run \
  -e PROVIDER_HTTP_URL="http://host.docker.internal:6060/v0_9" \
  -e PROVIDER_WS_URL="ws://host.docker.internal:6061/v0_9" \
  -e SIGNER_OPERATIONAL_ADDRESS="0x123" \
  -e SIGNER_EXTERNAL_URL="http://host.docker.internal:8080" \
  nethermind/starknet-staking-v2
```

Or use an environment file:

```bash
docker run --env-file .env nethermind/starknet-staking-v2
```

:::tip
Environment variables rank second in configuration precedence. Command line parameters override environment variables.
:::

## Configuration File

The validator can be configured using a JSON file:

### Sample JSON File

```json
{
  "provider": {
    "http": "http://localhost:6060/v0_9",
    "ws": "ws://localhost:6061/v0_9"
  },
  "signer": {
    "operationalAddress": "0x123",
    "url": "http://localhost:8080"
  }
}
```

To run the validator with a configuration file, use the `--config` option:

```bash
# Standalone binary
./build/validator --config config.json

# Docker container
docker run \
  -v /path/to/config.json:/app/config/config.json \
  nethermind/starknet-staking-v2 --config /app/config/config.json
```

:::warning Signing Method Required
You must provide either `privateKey` for internal signing or `url` for external signing. If both are provided, external signing will be used.
:::

## Mixed Configuration

You can combine multiple configuration methods. Values set by command line flags will override environment variables, and environment variables will override configuration file settings.

```bash
PROVIDER_HTTP_URL="http://localhost:6060/v0_9" ./build/validator \
    --config config.json \
    --provider-ws "ws://localhost:6061/v0_9" \
    --signer-url "http://localhost:8080" \
    --log-level debug
```

## Signing Methods

The validator supports two signing approaches:

### Internal Signing
- Provide your private key directly to the validator
- The validator signs transactions internally
- **⚠️ Security Note**: Only use this in secure environments

### External Signing  
- Provide a URL to an external signing service
- The validator requests signatures from the external service
- **✅ Recommended**: For mainnet and cloud deployments

:::tip Priority Note
If both `privateKey` and `url` are provided, the validator will prioritize external signing over internal signing.
:::

## What's Next?

- **[Configuration Options](./configuration-options)** - Complete reference of all available options
- **[External Signer](./external-signer)** - Secure signing for production deployments
