---
sidebar_position: 2
---

# Installation

```mdx-code-block
import Tabs from "@theme/Tabs";
import TabItem from "@theme/TabItem";
```

The Starknet Staking v2 validator can be installed using several methods. Choose the one that best fits your environment and needs.

<Tabs groupId="install">
<TabItem value="docker" label="Docker" default>

**1. Get the Docker image**

Docker images are published to the [nethermind/starknet-staking-v2](https://hub.docker.com/r/nethermind/starknet-staking-v2) repository on Docker Hub for `linux/amd64` and `linux/arm64`. Download the latest release:

```bash
docker pull nethermind/starknet-staking-v2
```

Each release is also tagged with its version, `nethermind/starknet-staking-v2:<version>`, where `<version>` is the release tag, such as `v0.3.0`. Pre-releases are tagged only with their version.

The image contains only the `validator` binary, with no shell and no package manager, and runs as a non-root user.

**2. Run the Docker container**

```bash
docker run --rm nethermind/starknet-staking-v2 --help
```

</TabItem>
<TabItem value="binary" label="Standalone binary">

**1. Download the archive**

Download the archive for your platform from the [releases page](https://github.com/NethermindEth/starknet-staking-v2/releases/latest):

| Platform | Archive |
| --- | --- |
| Linux amd64 | `starknet-staking-v2_<version>_Linux_x86_64.tar.gz` |
| Linux arm64 | `starknet-staking-v2_<version>_Linux_arm64.tar.gz` |
| macOS Intel | `starknet-staking-v2_<version>_Darwin_x86_64.tar.gz` |
| macOS Apple silicon | `starknet-staking-v2_<version>_Darwin_arm64.tar.gz` |

`checksums.txt` in the same release lists the SHA-256 checksum of each archive. For Windows, use the Docker image instead.

**2. Extract the archive**

```bash
tar -xzf starknet-staking-v2_<version>_Linux_x86_64.tar.gz
```

The archive contains the `validator` binary, already executable, along with `README.md` and `LICENSE`.

**3. Run the binary**

```bash
./validator --help
```

</TabItem>
<TabItem value="source" label="Build from source">

**Prerequisites**

- [Go](https://go.dev/doc/install) version `1.27.4` or above
- `make` and `git`

Building only the Docker image requires just [Docker](https://docs.docker.com/get-docker/).

**1. Clone the repository**

```bash
git clone https://github.com/NethermindEth/starknet-staking-v2.git
cd starknet-staking-v2
```

:::tip
You can use `git tag -l` to list the release tags and `git checkout <version>` to build one of them.
:::

**2. Build the binary**

```bash
make validator
```

The binary is written to `./build/validator`. Releases ship only the validator. To build the reference [external signer](./external-signer), run `make signer`, which writes `./build/signer`.

**3. Run the binary**

```bash
./build/validator --help
```

**4. Build and run the Docker image**

To run your build in Docker, build the image from the repository's `Dockerfile`:

```bash
docker build -t starknet-staking-v2 .
docker run --rm starknet-staking-v2 --help
```

</TabItem>
</Tabs>

:::tip
To learn how to set up the validator, check out the [Configuration](./configuration) guide.
:::
