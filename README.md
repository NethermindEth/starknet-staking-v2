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

### With a configuration file

The validator can be run with:
```bash
./build/validator --config <path_to_config_file> 
```

The config file is `.json` which specifies two types `provider` and `signer`. For the `provider`, it requires an *http* and *websocket* endpoints to a starknet node that supports rpc version `0.8.0` or higher. Those endpoints are used to listen information from the network.

For the `signer`, you need to speicfy the `operationalAddress` and either a `privateKey` or external `url`. By specifing your `privateKey` the program will sign the transactions using it. If you specify an `url` the program is going to ask through that `url` for the transaction to be signed. The only transaction that requires signing are the **attest** transactions.
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
      "operationalAddress": "0x123"
      "privateKey": "0x456", 
  }
}
```

If a signer is defined with both a private key and a external url, the program will give priority to the external signer over signing it itself.

### With flags

The same basics apply as described in the previous section. The following command runs the validator and provides all the necessary information about provider and signer:
```bash
./build/validator \
    --provider-http "http://localhost:6060/v0_8" \
    --provider-ws "ws://localhost:6061/v0_8" \
    --signer-url "http//localhost:8080" \
    --signer-op-address "0x123" \
    --signer-priv-key "0x456"
```

### With configuration file and flags

Using a combination of both approaches is also valid. In this case, the values provided by the flags override the values provided by the configuration file.

```bash
./build/validator \
    --config <path_to_config_file> \
    --provider-http "http://localhost:6060/v0_8" \
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
    "transaction_hash": "0x123"
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
  -d '{"transaction_hash": "0x567"}
```

You should get the following answer:
```json
{
    "signature": [
        "0x2534533c18797c67974111a5b79210574bfa4a98c2adc97fb5a06164da4b2ea",
        "0x434a779d865a617a7a47d1c48b7220dda230103ff1a006b752374f89d14f3ed"
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
      "operationalAddress": "your operational address"
      "privateKey": "your private key", 
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

