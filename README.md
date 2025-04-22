# Starknet Staking v2
Validator software written in Go for Starknet staking v2 as specified in [SNIP 28](https://community.starknet.io/t/snip-28-staking-v2-proposal/115250)


## Requirements

- A connection to a [Starknet node or RPC endpoint](https://www.starknet.io/fullnodes-rpc-services/) with support for the JSON-RPC 0.8.0 API specification. For reliability reasons we recommend stakers to host their own nodes. See [Juno](https://github.com/NethermindEth/juno) and [Pathfinder](https://github.com/eqlabs/pathfinder).
- An account with enough funds to pay for the attestation transactions.

## Installation

The tool can be either built from source or pulled from docker

### Build from Source

Requires having the [GO compiler](https://go.dev/doc/install) with version `1.24` or above. Once installed run:

```bash
make validator
```

This will compile the project and place the binary in *./build/validator*.

### Using docker

Make sure you've [Docker] installed and run:
```bash
docker pull nethermind/starknet-staking-v2
```

## Configuration and execution

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

The config file is `.json` which specifies two types `provider` and `signer`. For the `provider`, it requires an *http* and *websocket* endpoints to a starknet node that supports rpc version `0.8.0` or higher. Those endpoints are used to listen information from the network.

For the `signer`, you need to specify the `operationalAddress` and either a `privateKey` or external `url`. By specifing your `privateKey` the program will sign the transactions using it. If you specify an `url` the program is going to ask through that `url` for the transaction to be signed. The only transaction that requires signing are the **attest** transactions.
Through the use of an `url` for external signing the program remains agnostic over the users private key. The `url` should point to an *http* address through which this program and the signer program will communicate. The way this communication happens is specified [here](#external-signer).

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

#### With Docker

To run the validator using Docker, prepare a valid config file locally and mount it into the container:

```bash
docker run \
  -v <path_to_config_file>:/app/config/config.json \
  nethermind/starknet-staking-v2:latest --config /app/config/config.json 
```

### With Environment Variables
Similarly described as the previous section, the validator can be configured using environment vars. The following example using a `.env` file:

```bash
PROVIDER_HTTP_URL="http://localhost:6060/v0_8"
PROVIDER_WS_URL="http://localhost:6061/v0_8"

SIGNER_EXTERNAL_URL="http://localhost:8080"
SIGNER_OPERATIONAL_ADDRESS="0x123"
SIGNER_PRIVATE_KEY="0x456"
```

Then run
```bash
source path/to/env

./build/validator
```


### With flags
The following command runs the validator and provides all the necessary information about provider and signer through the use of flags:

```bash
./build/validator \
    --provider-http "http://localhost:6060/v0_8" \
    --provider-ws "ws://localhost:6061/v0_8" \
    --signer-url "http://localhost:8080" \
    --signer-op-address "0x123" \
    --signer-priv-key "0x456"
```

#### With Docker

To run the validator using Docker without a config file, just make sure to pass all the required flags.

```bash
docker run \
    nethermind/starknet-staking-v2:latest \
        --provider-http "http://localhost:6060/v0_8" \
        --provider-ws "ws://localhost:6061/v0_8" \
        --signer-url "http//localhost:8080" \
        --signer-op-address "0x123" \
        --signer-priv-key "0x456"
```

### Mixed configuration approach

Using a combination of both approaches is also valid. In this case, the values provided by the flags override the values by the environment vars which in turn override the values provided by the configuration file. 

```bash
PROVIDER_HTTP_URL="http://localhost:6060/v0_8" ./build/validator \
    --config <path_to_config_file> \
    --provider-ws "ws://localhost:6061/v0_8" \
    --signer-url "http//localhost:8080" \
    --signer-op-address "0x123" \
    --private-key "0x456"
```

## External Signer

To avoid users exposing their private keys our Validator program is capable of communicating with another process independent from the one provided here.

This external signer must implement a simple HTTP server that waits for `POST` requests on an endpoint of the form `<signer_address>/sign`. This `<signer_address>` is the same one that should be specified when initializing the validator tool in the `signer-url` flag.

The `POST` request will have the following form:
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

And answer with the ECDSA signature values `r` and `s` in an array:
```json
{
  "signature": [
    "0xabc",
    "0xdef"
  ]
}

```

We have provided a functional implementation [here](https://github.com/NethermindEth/starknet-staking-v2/tree/main/signer/signer.go) for you to try and use as an example if you want to implement your own.

### Try out our external signer

First make sure you compile it from source with:
```bash
make signer
```

Then execute it with:
```bash
SIGNER_PRIVATE_KEY="0x123" ./build/signer \
    --address localhost:8080
```

*On a separate terminal*, simulate the request for signing using the following request:
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

You should get the following answer:
```json
{
  "signature": [
    "0x12bf16c46782eb88570942ce126b2284bfb46b21c4b071a116bc0a6cffff35e",
    "0x69abdfe5ba5b24dbbb2b9ccc3c02b03f46c505d3aa8b37d3a4bb3d6b1a81ded"
  ]
}
```

This type of communication is exactly what will happen behind the curtains when using the validator tool and the signer each time there is an attestation required. This way you don't have to trust the software to protect your key.


## Logging

You have the possibility to give an additional flag `--log-level [info/debug/trace/warn/error]` to control the level of logging.
If not set, the log level will default to `info`.

## Example with Juno

Once you have your own node set either built from source or through docker. [See how](https://github.com/NethermindEth/juno?tab=readme-ov-file#run-with-docker).

Run your node with both the `http` and `ws` flags set. One example using Juno built from source:

```bash
./build/juno
  --db-path /var/lib/juno \
  --eth-node <YOUR-ETH-NODE>
  --http \
  --http-port 6060 \
  --ws \
  --ws-port 6061 \
```

The configuration file properties for local signing will look like:
```json
{
  "provider": {
      "http": "http://localhost:6060/v0_8",
      "ws": "ws://localhost:1235/v0_8"
  },
  "signer": {
      "operationalAddress": "your operational address",
      "privateKey": "your private key"
  }
}
```

## Contact us

We are the team behind the Juno client. Please don't hesitate to contact us if you have questions or feedback:

- [Telegram](https://t.me/StarknetJuno)
- [Discord](https://discord.com/invite/TcHbSZ9ATd)
- [X(Formerly Twitter)](https://x.com/NethermindStark)

##  License

Starknet Staking v2 is open-source software licensed under the [Apache-2.0 License](https://github.com/NethermindEth/starknet-staking-v2/blob/main/LICENSE).

