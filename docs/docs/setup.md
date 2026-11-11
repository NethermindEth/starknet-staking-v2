---
sidebar_position: 3
---

# Setup

The Starknet Staking v2 validator can be configured using several methods, with the following order of precedence:

1. **[Command line parameters](?config-source=cli#configuration-methods)**
2. **[Environment variables](?config-source=env#configuration-methods)**
3. **[Configuration file](?config-source=file#configuration-methods)**

:::tip Protocol Context
For detailed information about validator roles, address types, and security practices in Starknet's staking protocol, see the [official Starknet staking documentation](https://docs.starknet.io/architecture-and-concepts/staking/).
:::

## Configuration Methods

Each tab configures the same validator, so you can compare the methods side by side. The Docker examples use `--network host`, so `localhost` reaches the node and the signer running on the host.

```mdx-code-block
import Tabs from "@theme/Tabs";
import TabItem from "@theme/TabItem";
```

<Tabs groupId="config-source" queryString>
<TabItem value="cli" label="CLI" default>

The validator can be configured directly on the command line:

<Tabs groupId="install">
<TabItem value="docker" label="Docker" default>

```bash
docker run --network host nethermind/starknet-staking-v2 \
  --provider-http "http://localhost:6060/v0_10" \
  --provider-ws "ws://localhost:6061/v0_10" \
  --signer-op-address "0x123" \
  --signer-url "http://localhost:8080"
```

</TabItem>
<TabItem value="binary" label="Binary">

```bash
./validator \
  --provider-http "http://localhost:6060/v0_10" \
  --provider-ws "ws://localhost:6061/v0_10" \
  --signer-op-address "0x123" \
  --signer-url "http://localhost:8080"
```

</TabItem>
</Tabs>

:::tip
Command line parameters override environment variables and configuration file.
:::

</TabItem>
<TabItem value="env" label="Environment variables">

The validator can be configured through environment variables using specific variable names:

<Tabs groupId="install">
<TabItem value="docker" label="Docker" default>

```bash
docker run --network host \
  -e PROVIDER_HTTP_URL="http://localhost:6060/v0_10" \
  -e PROVIDER_WS_URL="ws://localhost:6061/v0_10" \
  -e SIGNER_OPERATIONAL_ADDRESS="0x123" \
  -e SIGNER_EXTERNAL_URL="http://localhost:8080" \
  nethermind/starknet-staking-v2
```

</TabItem>
<TabItem value="binary" label="Binary">

```bash
PROVIDER_HTTP_URL="http://localhost:6060/v0_10" \
PROVIDER_WS_URL="ws://localhost:6061/v0_10" \
SIGNER_OPERATIONAL_ADDRESS="0x123" \
SIGNER_EXTERNAL_URL="http://localhost:8080" \
./validator
```

</TabItem>
</Tabs>

You can also keep them in a `.env` file. Leave the values unquoted, because `docker run --env-file` keeps quotes as part of the value:

```bash title=".env"
PROVIDER_HTTP_URL=http://localhost:6060/v0_10
PROVIDER_WS_URL=ws://localhost:6061/v0_10
SIGNER_OPERATIONAL_ADDRESS=0x123
SIGNER_EXTERNAL_URL=http://localhost:8080
```

Then load the file and run the validator:

<Tabs groupId="install">
<TabItem value="docker" label="Docker" default>

```bash
docker run --network host --env-file .env nethermind/starknet-staking-v2
```

</TabItem>
<TabItem value="binary" label="Binary">

`set -a` exports every variable the file sets, so the validator inherits them:

```bash
set -a
source .env
set +a
./validator
```

</TabItem>
</Tabs>

:::tip
Environment variables rank second in configuration precedence. Command line parameters override environment variables.
:::

</TabItem>
<TabItem value="file" label="Config file">

The validator can be configured using a JSON file:

```json title="config.json"
{
  "provider": {
    "http": "http://localhost:6060/v0_10",
    "ws": "ws://localhost:6061/v0_10"
  },
  "signer": {
    "operationalAddress": "0x123",
    "url": "http://localhost:8080"
  }
}
```

To run the validator with a configuration file, use the `--config` option:

<Tabs groupId="install">
<TabItem value="docker" label="Docker" default>

```bash
docker run --network host \
  --user "$(id -u):$(id -g)" \
  -v /path/to/config.json:/app/config/config.json \
  nethermind/starknet-staking-v2 --config /app/config/config.json
```

The image runs as a non-root user. `--user` runs the validator as your own user instead, so it can read a file that only you can read, such as one holding a private key with mode `600`.

</TabItem>
<TabItem value="binary" label="Binary">

```bash
./validator --config config.json
```

</TabItem>
</Tabs>

</TabItem>
</Tabs>

## Mixed Configuration

You can combine multiple configuration methods. Values set by command line flags will override environment variables, and environment variables will override configuration file settings.

```bash
PROVIDER_HTTP_URL="http://localhost:6060/v0_10" ./validator \
    --config config.json \
    --provider-ws "ws://localhost:6061/v0_10" \
    --signer-url "http://localhost:8080" \
    --log-level debug
```

## Signing Methods

The validator supports two signing approaches:

1. **[Internal signing](?signing-method=internal#signing-methods)** where you provide your private key to the validator and it signs on your behalf.
2. **[External signing](?signing-method=external#signing-methods)** where the validator requests a remote service for your signature.
**[Read more](./external-signer)** about the remote signer's HTTP protocol.

<Tabs groupId="signing-method" queryString>
<TabItem value="internal" label="Internal signing" default>

<Tabs groupId="install">
<TabItem value="docker" label="Docker" default>

```bash
docker run --network host nethermind/starknet-staking-v2 \
  --provider-http "http://localhost:6060/v0_10" \
  --provider-ws "ws://localhost:6061/v0_10" \
  --signer-op-address "0x123" \
  --signer-priv-key "0x456"
```

</TabItem>
<TabItem value="binary" label="Binary">

```bash
./validator \
  --provider-http "http://localhost:6060/v0_10" \
  --provider-ws "ws://localhost:6061/v0_10" \
  --signer-op-address "0x123" \
  --signer-priv-key "0x456"
```

</TabItem>
</Tabs>

</TabItem>
<TabItem value="external" label="External signing">

<Tabs groupId="install">
<TabItem value="docker" label="Docker" default>

```bash
docker run --network host nethermind/starknet-staking-v2 \
  --provider-http "http://localhost:6060/v0_10" \
  --provider-ws "ws://localhost:6061/v0_10" \
  --signer-op-address "0x123" \
  --signer-url "http://localhost:8080"
```

</TabItem>
<TabItem value="binary" label="Binary">

```bash
./validator \
  --provider-http "http://localhost:6060/v0_10" \
  --provider-ws "ws://localhost:6061/v0_10" \
  --signer-op-address "0x123" \
  --signer-url "http://localhost:8080"
```

</TabItem>
</Tabs>


</TabItem>
</Tabs>

:::warning Signing Method Required
You must provide exactly one signing method: either a private key for internal signing (`--signer-priv-key`, `SIGNER_PRIVATE_KEY` or `privateKey`) or an external signer URL (`--signer-url`, `SIGNER_EXTERNAL_URL` or `url`). If both are provided, the validator won't start.
:::

## What's Next?

- **[Configuration Options](./configuration-options)** - Complete reference of all available options
- **[External Signer](./external-signer)** - Secure signing for production deployments
