---
sidebar_position: 6
---

# Metrics

The validator includes a built-in metrics server that exposes various metrics about the validator's operation. These metrics can be used to monitor the validator's performance and health.

## Configuration

By default, metrics are disabled. You can enable metrics tracking and customize the host and port with the following flags:

```mdx-code-block
import Tabs from "@theme/Tabs";
import TabItem from "@theme/TabItem";
```

<Tabs groupId="install">
<TabItem value="docker" label="Docker" default>

```bash
docker run --network host \
  --user "$(id -u):$(id -g)" \
  -v /path/to/config.json:/app/config/config.json \
  nethermind/starknet-staking-v2 \
  --config /app/config/config.json \
  --metrics --metrics-host "0.0.0.0" --metrics-port "9090"
```

</TabItem>
<TabItem value="binary" label="Binary">

```bash
./build/validator --metrics --metrics-host "0.0.0.0" --metrics-port "9090"  # Listen on all interfaces, port 9090
```

</TabItem>
</Tabs>

If the metrics server can't listen on that host and port (an invalid port, a port already in use), the validator exits with an error at startup.

## Endpoints

The metrics server exposes two endpoints:

- `/health`: Returns a 200 OK response if the server is running
- `/metrics`: Exposes Prometheus metrics

## Available Metrics

The following metrics are available:

```mdx-code-block
import Metrics from "./_metrics.md";
import ExpandableTable from "@site/src/components/ExpandableTable";

<ExpandableTable>
<Metrics />
</ExpandableTable>
```

Every metric has a `network` label with the chain id of the provider's network, such as `SN_MAIN` or `SN_SEPOLIA`. Metrics with an `account` label report `signer` or `reward` in it. A scrape of `/metrics` returns lines such as:

```text
validator_attestation_current_epoch_id{network="SN_SEPOLIA"} 42
```

Reading the counters:

- An increase of `validator_attestation_epoch_info_fetch_failure_count` means the validator is stalled, since block headers are not processed while the fetch fails.
- While `validator_attestation_balance_check_failure_count` grows, the balance metrics of that account keep their last value.
- A reward claim that is submitted but neither confirmed nor counted as a failure was not accepted within 2 minutes: check its status manually.

:::info Reward metrics
All the `validator_attestation_reward_*` metrics are only reported if the [auto-claim rewards](./auto-claim) feature is enabled. The reward account balance metrics are not reported when the reward account is the operational account, whose balance is already reported by the signer metrics.
:::

## Using with Prometheus

To monitor these metrics with Prometheus, add the following to your Prometheus configuration:

```yaml
scrape_configs:
  - job_name: 'starknet-validator'
    scrape_interval: 15s
    static_configs:
      - targets: ['localhost:9090']
```

You can then visualize these metrics using Grafana or any other Prometheus-compatible visualization tool.

## Grafana Dashboard

A sample Grafana dashboard is available to visualize the validator metrics: [grafana-dashboard.json](/grafana-dashboard.json)

This dashboard provides comprehensive monitoring of your validator's performance including:
- Block and epoch progression
- Attestation success rates  
- Network status and health metrics 
