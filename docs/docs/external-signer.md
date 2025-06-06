---
sidebar_position: 5
---

# External Signer

> This section explains how the external signer works. If you don't plan to run the validator in an unsafe environment (such as the cloud) you probably don't need it.

To avoid users exposing their private keys the validator program has a simple communication protocol implemented via HTTP requests for remote/external signing.

## HTTP Protocol

The external signer must implement a simple HTTP server that waits for `POST` requests on an endpoint of the form `<signer_address>/sign`. When initializing the validator the `<signer_address>` should be specified in its configuration (e.g. specifying `--signer-url` flag).

### Request Format

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

### Response Format

It will wait for ECDSA signature values `r` and `s` in an array:

```json
{
  "signature": [
    "0xabc",
    "0xdef"
  ]
}
```

We have provided an already functional implementation for you to use or take as an example to implement your own.

## Example

This example simulates the interaction between validator and remote signer using our own implemented signer. Start by compiling the remote signer:

```bash
make signer
```

Then set a private key which will be used to sign transactions and the HTTP address where the signer will receive POST requests from the validator program. For example using private key `0x123`:

```bash
SIGNER_PRIVATE_KEY="0x123" ./build/signer \
    --address localhost:8080
```

This will start the program and will remain there listening for requests.

**On a separate terminal**, send a transaction data and request its signing. For example:

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

You should immediately get the following answer provided you used the same private key and transaction data shown as an example:

```json
{
  "signature": [
    "0x12bf16c46782eb88570942ce126b2284bfb46b21c4b071a116bc0a6cffff35e",
    "0x69abdfe5ba5b24dbbb2b9ccc3c02b03f46c505d3aa8b37d3a4bb3d6b1a81ded"
  ]
}
```

This communication is what will happen behind the curtains when using the validator and an external signer each time there is an attestation required. Notice that the validator program remains completely agnostic to the private key since only the remote signer knows it. 