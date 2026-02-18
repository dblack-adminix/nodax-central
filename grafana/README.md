# Grafana templates for NODAX Central

This folder contains starter templates to visualize NODAX Central metrics and logs.

## 1) Metrics datasource (Prometheus)

NODAX Central now exposes Prometheus metrics at:

- `http://<central-host>:8080/metrics`

Add this target to your Prometheus config:

```yaml
scrape_configs:
  - job_name: nodax-central
    metrics_path: /metrics
    static_configs:
      - targets: ["localhost:8080"]
```

In Grafana:
1. Add Prometheus datasource.
2. Import dashboard from `dashboards/nodax-central-overview.json`.
3. Select your Prometheus datasource during import.

## 2) Logs datasource (JSON via Infinity plugin)

NODAX Central exposes normalized logs API for Grafana:

- `GET /api/grafana/logs`
- Example: `http://<central-host>:8080/api/grafana/logs?limit=500`

Supported query params:
- `limit` (1..5000)
- `agentId`
- `type`
- `status`
- `from` (RFC3339 or `YYYY-MM-DD HH:mm:ss`)
- `to` (RFC3339 or `YYYY-MM-DD HH:mm:ss`)

Response format:

```json
{
  "items": [
    {
      "agentId": "agent_...",
      "agentName": "HV-01",
      "timestamp": "2026-02-14T10:20:30Z",
      "unixMs": 1739538030000,
      "type": "backup",
      "targetVm": "dc01",
      "status": "ok",
      "message": "backup completed"
    }
  ],
  "count": 1
}
```

For logs visualization in Grafana:
1. Install Grafana Infinity datasource plugin (`yesoreyeram-infinity-datasource`).
2. Create Infinity datasource.
3. Import dashboard from `dashboards/nodax-central-logs-infinity.json`.
4. Set URL in dashboard variable if needed.

## Exported metrics

- `nodax_central_agents_total`
- `nodax_central_agents_online`
- `nodax_host_up{agent_id,agent_name}`
- `nodax_host_cpu_usage_percent{agent_id,agent_name}`
- `nodax_host_ram_usage_percent{agent_id,agent_name}`
- `nodax_host_vm_total{agent_id,agent_name}`
- `nodax_host_vm_running{agent_id,agent_name}`
- `nodax_host_uptime_seconds{agent_id,agent_name}`
- `nodax_host_disk_usage_percent{agent_id,agent_name,drive}`
