# Starknet Staking v2
Validator software written in Go for Starknet staking v2 as specified in [SNIP 28](https://community.starknet.io/t/snip-28-staking-v2-proposal/115250)


## Requirements

- A connection to a [Starknet node or RPC endpoint](https://www.starknet.io/fullnodes-rpc-services/) with support for the JSON-RPC 0.8.0 API specification. For reliability reasons we recommend stakers to host their own nodes. See [Juno](https://github.com/NethermindEth/juno) and [Pathfinder](https://github.com/eqlabs/pathfinder).
- An account with enough funds to pay for the attestation transactions.

## Installation

Requires having the [GO compiler](https://go.dev/doc/install) with version `1.24` or above. Once installed run:

```bash
make validator
```

This will compile the project and place the binary in *./build/validator*.

## Running

To run the validator it needs certain data specified such as the node to connect to and the operational address of the staker. This data can be provided in two ways, either through a configuration file or through flags directly in the app.

### With a config file

After compiling the validator, run:
```bash
./build/validator --config <path_to_config_file> 
```

The config file is `.json` which specify two types `provider` and `signer`. For the `provider`, it requires an http and ws endpoints to a starknet node that supports rpc version `>=0.8.0`. For the `signer`, you can use our implementation provided in this program or one implemented by you.

Depending of the fields `signer` has set, it is stablished as either an internal (provided by us) or external (provided by you). An internal signer has the `operationalAddress` and `privateKey` values set while an external one has the `operationalAddress` and `url` values set. The `url` must point to an address through which validating software and signer will communicate.

A full config file would look like this:

```json
{
  "provider": {
      "http": "http://localhost:6060/v0_8",
      "ws": "ws://localhost:6061/v0_8"
  },
  "signer": {
      "url": "http://localhost:8080/v0_8",
      "operationalAddress": "0x123"
      "privateKey": "0x456", 
  }
}
```

If a signer is defined with both private key and external url, the program will assume that an external signer is intended.

### With flags

The same basics applies as the previous section. Run the validator with the following command:
```bash
./build/validator \
    --provider-http "http://localhost:6060/v0_8" \
    --provider-ws "ws://localhost:60601/v0_8" \
    --signer-url "http//localhost:8080/v0_8" \
    --signer-op-address "0x123" \
    --private-key "0x456"
```

## External Signer 

The external signer must implement a simple HTTP API, exposing a single `/sign` endpoint:

- POST `/sign`: should return the signature for the transaction hash received as its input:
```json
{
    "transaction_hash": "0x123"
}
```
Response should contain the ECDSA signature values r and s in an array:
```json
{
  "signature": [
    "0xabc",
    "0xdef"
  ]
}
```
An example implementation is provided [here](https://github.com/NethermindEth/starknet-staking-v2/tree/main/example-signer/remote_signer.go).

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

The configuration file properties will look like:
```json
{
  "provider": {
      "http": "http://localhost:6060/v0_8",
      "ws": "ws://localhost:1235/v0_8"
  },
  "signer": {
      "operationalAddress": "your operational address"
      "privateKey": "your private key", 
  }
}
```

##  License

Starknet Staking v2 is open-source software licensed under the [Apache-2.0 License](https://github.com/NethermindEth/starknet-staking-v2/blob/main/LICENSE).

