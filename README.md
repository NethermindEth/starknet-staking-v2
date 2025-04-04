# Starknet Staking v2
Validator software written in Go for Starknet staking v2 as specified in [Snip28](https://community.starknet.io/t/snip-28-staking-v2-proposal/115250)


## Requirements

- A connection to a Starknet node with support for the JSON-RPC 0.8.0 API specification. This tool has been tested with Juno.
- Sufficient funds in the operational account to pay for attestation transactions.


## Running

Use the following command:
```bash
# TODO: should we change cobra command's "Use" field, which is now "starknet-staking-v2" ?
# TODO: I guess we should upload the binary somewhere and/or use docker
# TODO: should we take the Staking & Attest contract addresses as well ?
# TODO: create different log levels ?
./build/validator --config <path_to_config_file>
```

The config file must contain the following fields:
```json
{
    "privateKey": "<private_key>",
    // TODO: we are able to deduct the public key from the private key, maybe remove it from the config
    "publicKey": "<public_key>",
    "address": "<operational_address>",
    "httpProviderUrl": "<http_provider_url>",
    "wsProviderUrl": "<ws_provider_url>"
}
```

## ⚠️ License

Copyright (c) 2022-present, with the following [contributors](https://github.com/NethermindEth/starknet-staking-v2/graphs/contributors).

Starknet Staking v2 is open-source software licensed under the [Apache-2.0 License](https://github.com/NethermindEth/starknet-staking-v2/blob/main/LICENSE).

