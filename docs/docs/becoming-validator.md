---
sidebar_position: 1.5
---

# Become a Staking Validator

Staking on Starknet provides an opportunity to contribute to network security and earn rewards by becoming a validator. Check out the [Becoming a Validator](https://docs.starknet.io/staking/entering-staking/) guide to learn more about the validator process.

## Prerequisites

- **STRK Tokens**: At least 20,000 STRK is required for staking on mainnet (1 STRK on Sepolia testnet). For the latest details, check out the [Staking Protocol Details](https://docs.starknet.io/staking/overview/#protocol_details).
- **Node Setup**: A connection to a [Starknet node or RPC endpoint](https://www.starknet.io/fullnodes-rpc-services/) with support for the JSON-RPC 0.9.0 API specification. For reliability reasons we recommend stakers to host their own nodes. See [Juno](https://github.com/NethermindEth/juno) and [Pathfinder](https://github.com/eqlabs/pathfinder).
- **Starknet Wallet**: A compatible wallet, like [Braavos](https://braavos.app/wallet-features/ledger-on-braavos/) or [Argent](https://www.argent.xyz/blog/how-to-use-your-hardware-wallet-with-argent).
- **Access to CLI/Block Explorer**: Tools like [Voyager](https://voyager.online) for interacting with contracts.
- **An account with enough funds** to pay for the attestation transactions.

## 1. Set up your Starknet node

A reliable Starknet node is essential for validator operation. Follow the official [Juno documentation](https://juno.nethermind.io/) to configure your node using Docker, binaries, source builds.

### Example with Juno

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
      "http": "http://localhost:6060/v0_9",
      "ws": "ws://localhost:6061/v0_9"
  },
  "signer": {
      "operationalAddress": "your operational address",
      "privateKey": "your private key"
  }
}
```

## 2. Stake STRK tokens

Register as a validator by staking STRK tokens through the Starknet staking contract. Check out the [Becoming a Validator](https://docs.starknet.io/staking/entering-staking/) guide for complete instructions. The staking process includes:

- **Pre-approving STRK Transfer**: Allow the staking contract to lock your tokens.
- **Calling the `stake` Function**: Register operational and reward addresses, set commission rates, and enable pooling if desired.

## 3. Finalising your validator

Once your node is running and your STRK tokens are staked:

1. **Install the validator**: Follow the [Installation](./installation) guide to install the validator software.
2. **Configure and start the validator**: Use your operational address in the [Configuration](./configuration) guide.
3. Monitor your validator's status via dashboards like [Voyager](https://voyager.online/).

:::info
You're now a staking validator! With your node running and tokens staked, you support Starknet's security and earn rewards. The network manages most operations, though future updates may require additional setup.
::: 