# Starknet Staking v2
Validator software written in Go for Starknet staking v2 as specified in [SNIP 28](https://community.starknet.io/t/snip-28-staking-v2-proposal/115250)


## Requirements

- A connection to a [Starknet node or RPC endpoint](https://www.starknet.io/fullnodes-rpc-services/) with support for the JSON-RPC 0.8.0 API specification. For reliability reasons we recommend stakers to host their own nodes. See [Juno](https://github.com/NethermindEth/juno) and [Pathfinder](https://github.com/eqlabs/pathfinder).
- An account with enough funds to pay for the attestation transactions.

## Installation

The tool can be either built from source or pulled from docker. Aditionally we offer pre-compiles, check our [release page](https://github.com/NethermindEth/starknet-staking-v2/releases).

### Building from source

Requires having the [GO compiler](https://go.dev/doc/install) with version `1.24` or above. Once installed run:

```bash
make validator
```

This will compile the project and place the binary in *./build/validator*.

### Using docker

Make sure you've [Docker](https://www.docker.com/) installed and run:
```bash
docker pull nethermind/starknet-staking-v2
```

## Validator configuration and execution

To run the validator it needs certain data specified such as the Starknet node to connect to and the operational address of the staker.
This data can be provided through several ways, in order of (decreasing) priority:
1. Command line flags,
2. Environment vars and
3. Configuration file.

### With a configuration file

The validator can be run with:
```bash
./build/validator --config <path_to_config_file>
```

The config file is `.json` which specifies two main fields `provider` and `signer`. For the `provider`, it requires an *http* and *websocket* endpoints to a starknet node that supports rpc version `0.8.1` or higher. Those endpoints are used to listen information from the network.

For the `signer`, you need to specify the *operational address* and a signing method. 
The signing method can be either internal to the tool or asked externally, based on if you provide a *private key* or an external *url*:
1. By provding a *private key* the program will sign the transactions internally.
2. By providing an external *url* to program from which the validator will ask for signatures, see exactly how [here](#external-signer).
3. If both are provided, the validator will use the remote signer over the internal one.


A full configuration file looks like this:

```json
{
  "provider": {
      "http": "http://localhost:6060/v0_8",
      "ws": "ws://localhost:6061/v0_8"
  },
  "signer": {
      "url": "http://localhost:8080",
      "operationalAddress": "0x123",
      "privateKey": "0x456"
  }
}
```

Note that because both `url` and `privateKey` fields are set in the previous example the tool will prioritize remote signing through the `url` than internally signing with the `privateKey`. Be sure to  be explicit on your configuration file and leave just one of them.

#### Example with Docker

To run the validator using Docker, prepare a valid config file locally and mount it into the container:

```bash
docker run \
  -v <path_to_config_file>:/app/config/config.json \
  nethermind/starknet-staking-v2:latest --config /app/config/config.json 
```

### With Environment Variables
Alternatively, similarly as described as the previous section, the validator can be configured using environment vars. The following example using a `.env` file with the following content:

```bash
PROVIDER_HTTP_URL="http://localhost:6060/v0_8"
PROVIDER_WS_URL="http://localhost:6061/v0_8"

SIGNER_EXTERNAL_URL="http://localhost:8080"
SIGNER_OPERATIONAL_ADDRESS="0x123"
SIGNER_PRIVATE_KEY="0x456"
```

Source the enviroment vars and run the validator:

```bash
source path/to/env

./build/validator
```


### With flags
Finally, as a third alternative, you can specify the necessary validation configuration through flags as well:

```bash
./build/validator \
    --provider-http "http://localhost:6060/v0_8" \
    --provider-ws "ws://localhost:6061/v0_8" \
    --signer-url "http://localhost:8080" \
    --signer-op-address "0x123" \
    --signer-priv-key "0x456"
```


### Mixed configuration approach

Using a combination of both approaches is also valid. Values set by flags will override values set by enviroment flags and values set by enviroment flags will override values set in a configuration file.

```bash
PROVIDER_HTTP_URL="http://localhost:6060/v0_8" ./build/validator \
    --config <path_to_config_file> \
    --provider-ws "ws://localhost:6061/v0_8" \
    --signer-url "http//localhost:8080" \
    --signer-op-address "0x123" \
    --private-key "0x456"
```

## Additional configurations

In addition to the configuration described above, the tool allows for other non-essential customization. You can see all available options by using the `--help` flag:

1. Using specific staking and attestation contract addresses through the `--staking-contract-address` and `--attest-contract-address` flags respectively. If no values are provided, sensible defaults are provided based on the network id.

2. `--max-tries` allows you to set how many attempts the tool does to get attestation information. It can be set to any positive number or to _"infinite"_ if you want the tool to never stop execution. Defaults to 10.

3. `--log-level` set's the tool logging level. Default to `info`.

4. `--braavos-account` changes the transaction version format from `0x3` to `1<<128 + 0x3` required by Braavos accounts. _Note that this is still an experimental feature_.

## Metrics

The validator includes a built-in metrics server that exposes various metrics about the validator's operation. These metrics can be used to monitor the validator's performance and health.

### Configuration

By default, metrics are disabled. You can enable metrics tracking and customize the host and port with the following flags:

```bash
./build/validator --metrics --metrics-host "0.0.0.0" --metrics-port "9090"  # Listen on all interfaces, port 9090
```

### Endpoints

The metrics server exposes two endpoints:

- `/health`: Returns a 200 OK response if the server is running
- `/metrics`: Exposes Prometheus metrics

### Available Metrics

The following metrics are available:

| Metric Name | Type | Description | Example |
|-------------|------|-------------|---------|
| `validator_attestation_starknet_latest_block_number` | Gauge | The latest block number seen by the validator on the Starknet network | `validator_attestation_starknet_latest_block_number{network="SN_SEPOLIA"} 10500` |
| `validator_attestation_current_epoch_id` | Gauge | The ID of the current epoch the validator is participating in | `validator_attestation_current_epoch_id{network="SN_SEPOLIA"} 42` |
| `validator_attestation_current_epoch_length` | Gauge | The total length (in blocks) of the current epoch | `validator_attestation_current_epoch_length{network="SN_SEPOLIA"} 100` |
| `validator_attestation_current_epoch_starting_block_number` | Gauge | The first block number of the current epoch | `validator_attestation_current_epoch_starting_block_number{network="SN_SEPOLIA"} 10401` |
| `validator_attestation_current_epoch_assigned_block_number` | Gauge | The specific block number within the current epoch for which the validator is assigned to attest | `validator_attestation_current_epoch_assigned_block_number{network="SN_SEPOLIA"} 10455` |
| `validator_attestation_last_attestation_timestamp_seconds` | Gauge | The Unix timestamp (in seconds) of the last successful attestation submission | `validator_attestation_last_attestation_timestamp_seconds{network="SN_SEPOLIA"} 1678886400` |
| `validator_attestation_attestation_submitted_count` | Counter | The total number of attestations submitted by the validator since startup | `validator_attestation_attestation_submitted_count{network="SN_SEPOLIA"} 55` |
| `validator_attestation_attestation_failure_count` | Counter | The total number of attestation transaction submission failures encountered by the validator since startup | `validator_attestation_attestation_failure_count{network="SN_SEPOLIA"} 3` |
| `validator_attestation_attestation_confirmed_count` | Counter | The total number of attestations that have been confirmed on the network since validator startup | `validator_attestation_attestation_confirmed_count{network="SN_SEPOLIA"} 52` |

All metrics include a `network` label that indicates the Starknet network (e.g., "SN_MAINNET", "SN_SEPOLIA").

### Using with Prometheus

To monitor these metrics with Prometheus, add the following to your Prometheus configuration:

```yaml
scrape_configs:
  - job_name: 'starknet-validator'
    scrape_interval: 15s
    static_configs:
      - targets: ['localhost:9090']
```

You can then visualize these metrics using Grafana or any other Prometheus-compatible visualization tool.


## Example with Juno

Once you have your own [Juno](https://github.com/NethermindEth/juno) node set either built from source or through docker. 

Run it and be sure to specify both `http` and `ws` flags set. These prepare your node to receive both *http* and *websocket* requests, required by the validator for full communication with the node.
One example using a Juno binary built from source:

```bash
./build/juno
  --db-path /var/lib/juno \
  --eth-node <YOUR-ETH-NODE>
  --http \
  --http-port 6060 \
  --ws \
  --ws-port 6061 \
```

The configuration file properties for internal signing will look like:
```json
{
  "provider": {
      "http": "http://localhost:6060/v0_8",
      "ws": "ws://localhost:6061/v0_8"
  },
  "signer": {
      "operationalAddress": "your operational address",
      "privateKey": "your private key"
  }
}
```

## External Signer

> This section explains how the external signer works. If you don't plan to run the validator on an unsafe enviroment (such as the cloud) you probably don't need it.
 
To avoid users exposing their private keys the validator program has a simple communication protocol implemented via http requests for remote/external signing.

The external signer must implement a simple HTTP server that waits for `POST` requests on an endpoint of the form `<signer_address>/sign`. When initializing the validator the `<signer_address>` should be specified in it's configuration (e.g. specifying `--signer-url` flag).

The validator will make `POST` request with all the transaction data to sign:
```json
{
  "transaction": {
    "type": "INVOKE",
    "sender_address": "0x11efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e",
    "calldata": [
      "0x1",
      "0x4862e05d00f2d0981c4a912269c21ad99438598ab86b6e70d1cee267caaa78d",
      "0x37446750a403c1b4014436073cf8d08ceadc5b156ac1c8b7b0ca41a0c9c1c54",
      "0x1",
      "0x6521dd8f51f893a8580baedc249f1afaf7fd999c88722e607787970697dd76"
    ],
    "version": "0x3",
    "signature": [
      "0x6711bfe51870a9874af883ca974b94fef85200d5772db5792013644dc9dd16a",
      "0x23e345b39ffc43a92ab6735bf3f11e7dac1aa931d8c9647a1a2957f759b8baa"
    ],
    "nonce": "0x194",
    "resource_bounds": {
      "l1_gas": {
        "max_amount": "0x0",
        "max_price_per_unit": "0x57e48bc504e79"
      },
      "l1_data_gas": {
        "max_amount": "0x450",
        "max_price_per_unit": "0xa54"
      },
      "l2_gas": {
        "max_amount": "0xc92ca0",
        "max_price_per_unit": "0x1b5aea1cb"
      }
    },
    "tip": "0x0",
    "paymaster_data": [],
    "account_deployment_data": [],
    "nonce_data_availability_mode": "L1",
    "fee_data_availability_mode": "L1"
  },
  "chain_id": "0x534e5f5345504f4c4941"
}
```

It will wait for a ECDSA signature values `r` and `s` in an array:
```json
{
  "signature": [
    "0xabc",
    "0xdef"
  ]
}

```

We have provided an already functional implementation [here](https://github.com/NethermindEth/starknet-staking-v2/tree/main/signer/signer.go) for you to use or take as an example to implement your own.

### Example

This is example simulates the interaction validator and remote signer using our own implemented signer. Start by compiling the remote signer:

```bash
make signer
```

Then set a private key which will be used to sign transactions and the http address where the signer will recieve post requests from the validator program. For example using private key `0x123`:

```bash
SIGNER_PRIVATE_KEY="0x123" ./build/signer \
    --address localhost:8080
```

This will start the program and will remain there listening for requests.

*On a separate terminal*, send a transaction data and requests it's signing. For example:
```bash
curl -X POST http://localhost:8080/sign \
  -H "Content-Type: application/json" \
  -d '{
    "transaction": {
      "type": "INVOKE",
      "sender_address": "0x11efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e",
      "calldata": [
        "0x1",
        "0x4862e05d00f2d0981c4a912269c21ad99438598ab86b6e70d1cee267caaa78d",
        "0x37446750a403c1b4014436073cf8d08ceadc5b156ac1c8b7b0ca41a0c9c1c54",
        "0x1",
        "0x6521dd8f51f893a8580baedc249f1afaf7fd999c88722e607787970697dd76"
      ],
      "version": "0x3",
      "signature": [
        "0x6711bfe51870a9874af883ca974b94fef85200d5772db5792013644dc9dd16a",
        "0x23e345b39ffc43a92ab6735bf3f11e7dac1aa931d8c9647a1a2957f759b8baa"
      ],
      "nonce": "0x194",
      "resource_bounds": {
        "l1_gas": {
          "max_amount": "0x0",
          "max_price_per_unit": "0x57e48bc504e79"
        },
        "l1_data_gas": {
          "max_amount": "0x450",
          "max_price_per_unit": "0xa54"
        },
        "l2_gas": {
          "max_amount": "0xc92ca0",
          "max_price_per_unit": "0x1b5aea1cb"
        }
      },
      "tip": "0x0",
      "paymaster_data": [],
      "account_deployment_data": [],
      "nonce_data_availability_mode": "L1",
      "fee_data_availability_mode": "L1"
    },
    "chain_id": "0x534e5f5345504f4c4941"
  }'
```

You should immediatly get the following answer provided you used the same private key and transaction data showed as an example:
```json
{
  "signature": [
    "0x12bf16c46782eb88570942ce126b2284bfb46b21c4b071a116bc0a6cffff35e",
    "0x69abdfe5ba5b24dbbb2b9ccc3c02b03f46c505d3aa8b37d3a4bb3d6b1a81ded"
  ]
}
```

This communication is what will happen behind the curtains when using the validator and an external signer each time there is an attestation required. Notice that the validator program remains completly agnostic to the private key since only the remote signer knows it.


## Contact us

We are the team behind the Juno client. Please don't hesitate to contact us if you have questions or feedback:

- [Telegram](https://t.me/StarknetJuno)
- [Discord](https://discord.com/invite/TcHbSZ9ATd)
- [X(Formerly Twitter)](https://x.com/NethermindStark)

##  License

Starknet Staking v2 is open-source software licensed under the [Apache-2.0 License](https://github.com/NethermindEth/starknet-staking-v2/blob/main/LICENSE).

