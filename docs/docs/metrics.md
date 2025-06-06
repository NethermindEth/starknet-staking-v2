---
sidebar_position: 6
---

# Metrics

The validator includes a built-in metrics server that exposes various metrics about the validator's operation. These metrics can be used to monitor the validator's performance and health.

## Configuration

By default, metrics are disabled. You can enable metrics tracking and customize the host and port with the following flags:

```bash
./build/validator --metrics --metrics-host "0.0.0.0" --metrics-port "9090"  # Listen on all interfaces, port 9090
```

## Endpoints

The metrics server exposes two endpoints:

- `/health`: Returns a 200 OK response if the server is running
- `/metrics`: Exposes Prometheus metrics

## Available Metrics

The following metrics are available:

| Metric Name | Type | Description | Example |
|-------------|------|-------------|---------|
| `validator_attestation_starknet_latest_block_number` | Gauge | The latest block number seen by the validator on the Starknet network | `validator_attestation_starknet_latest_block_number{network="SN_SEPOLIA"} 10500` |
| `validator_attestation_current_epoch_id` | Gauge | The ID of the current epoch the validator is participating in | `validator_attestation_current_epoch_id{network="SN_SEPOLIA"} 42` |
| `validator_attestation_current_epoch_length` | Gauge | The total length (in blocks) of the current epoch | `validator_attestation_current_epoch_length{network="SN_SEPOLIA"} 100` |
| `validator_attestation_current_epoch_starting_block_number` | Gauge | The first block number of the current epoch | `validator_attestation_current_epoch_starting_block_number{network="SN_SEPOLIA"} 10401` |
| `validator_attestation_current_epoch_assigned_block_number` | Gauge | The specific block number within the current epoch for which the validator is assigned to attest | `validator_attestation_current_epoch_assigned_block_number{network="SN_SEPOLIA"} 10455` |
| `validator_attestation_last_attestation_timestamp_seconds` | Gauge | The Unix timestamp (in seconds) of the last successful attestation submission | `validator_attestation_last_attestation_timestamp_seconds{network="SN_SEPOLIA"} 1678886400` |
| `validator_attestation_attestation_submitted_count` | Counter | The total number of attestations submitted by the validator since startup | `validator_attestation_attestation_submitted_count{network="SN_SEPOLIA"} 55` |
| `validator_attestation_attestation_failure_count` | Counter | The total number of attestation transaction submission failures encountered by the validator since startup | `validator_attestation_attestation_failure_count{network="SN_SEPOLIA"} 3` |
| `validator_attestation_attestation_confirmed_count` | Counter | The total number of attestations that have been confirmed on the network since validator startup | `validator_attestation_attestation_confirmed_count{network="SN_SEPOLIA"} 52` |

All metrics include a `network` label that indicates the Starknet network (e.g., "SN_MAINNET", "SN_SEPOLIA").

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