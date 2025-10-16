---
sidebar_position: 2
---

# Installation

The Starknet Staking v2 validator can be installed using several methods. Choose the one that best fits your environment and needs.

## Pre-compiled Binaries

The easiest way to get started is by downloading pre-compiled binaries from our [release page](https://github.com/NethermindEth/starknet-staking-v2/releases).

1. Visit the [releases page](https://github.com/NethermindEth/starknet-staking-v2/releases)
2. Download the appropriate binary for your operating system
3. Make the binary executable (on Unix systems):

   ```bash
   chmod +x validator
   ```

4. Run the validator:

   ```bash
   ./validator --help
   ```

## Building from Source

### Prerequisites

- [Go compiler](https://go.dev/doc/install) version `1.25` or above

### Build Steps

1. Clone the repository:

   ```bash
   git clone https://github.com/NethermindEth/starknet-staking-v2.git
   cd starknet-staking-v2
   ```

2. Build the validator:

   ```bash
   make validator
   ```

3. The binary will be available at `./build/validator`

4. Verify the installation:

   ```bash
   ./build/validator --help
   ```

## Using Docker

### Prerequisites

- [Docker](https://www.docker.com/) installed and running

### Pull the Image

```bash
docker pull nethermind/starknet-staking-v2
```

### Run with Docker

```bash
docker run nethermind/starknet-staking-v2 --help
```

