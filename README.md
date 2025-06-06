# Starknet Staking v2
Validator software written in Go for Starknet staking v2 as specified in [SNIP 28](https://community.starknet.io/t/snip-28-staking-v2-proposal/115250)

> ℹ️ This README offers a basic overview of the Validator tool. For detailed instructions—such as running with Docker, configuring external signing, or setting up a metrics server—please refer to the [official documentation](https://nethermindeth.github.io/starknet-staking-v2/).

## Requirements

- A connection to a [Starknet node or RPC endpoint](https://www.starknet.io/fullnodes-rpc-services/) with support for the JSON-RPC 0.8.0 API specification. For reliability reasons we recommend stakers to host their own nodes. See [Juno](https://github.com/NethermindEth/juno) and [Pathfinder](https://github.com/eqlabs/pathfinder).
- An account with enough funds to pay for the attestation transactions.

## Installation

The tool can be either built from source or pulled from docker. Aditionally we offer pre-compiles, check our [release page](https://github.com/NethermindEth/starknet-staking-v2/releases).

### Building from source

Requires having the latest [GO compiler](https://go.dev/doc/install) version. Once installed run:

```bash
make validator
```

This will compile the project and place the binary in *./build/validator*.

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
PROVIDER_WS_URL="ws://localhost:6061/v0_8"

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

## Other features

We offer other utilies such as [external signing](https://nethermindeth.github.io/starknet-staking-v2/external-signer) and [monitoring via prometheus](https://nethermindeth.github.io/starknet-staking-v2/metrics). 

Check our [documentation](https://nethermindeth.github.io/starknet-staking-v2/) for full details. 



## Contact us

We are the team behind the Juno client. Please don't hesitate to contact us if you have questions or feedback:

- [Telegram](https://t.me/StarknetJuno)
- [Discord](https://discord.com/invite/TcHbSZ9ATd)
- [X(Formerly Twitter)](https://x.com/NethermindStark)

##  License

Starknet Staking v2 is open-source software licensed under the [Apache-2.0 License](https://github.com/NethermindEth/starknet-staking-v2/blob/main/LICENSE).

