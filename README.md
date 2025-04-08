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

For executing the validator, just run:
```bash
./build/validator --config <path_to_config_file>
```

The config file must be a `.json` with the following fields:

1. `privateKey`: For signing transactions from the operational address
2. `operationalAddress`: Account address from where to do the attestation
3. `httpProviderUrl`: Used to send the invoke transactions to the sequencer 
4. `wsProviderUrl`:  Used to subscribe to the block headers


```json
{
    "privateKey": "<private_key>", 
    "operationalAddress": "<operational_address>",
    "httpProviderUrl": "<http_provider_url>", 
    "wsProviderUrl": "<ws_provider_url>" 
}
```


### Example with Juno

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
    "httpProviderUrl": "http://localhost:6060/v0_8",
    "wsProviderUrl": "ws://localhost:6061/v0_8"
}
```

It's important we specify the `v0_8` part so that we are routed through the right rpc version and not the node's default one.

## ⚠️ License

Starknet Staking v2 is open-source software licensed under the [Apache-2.0 License](https://github.com/NethermindEth/starknet-staking-v2/blob/main/LICENSE).

