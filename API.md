# FastProbe Client API Documentation

FastProbeServer exposes an API endpoint for client nodes to report their system configuration and current status. This document describes how a client script (intended primarily for Linux, but also supporting Windows and macOS) can interact with the FastProbeServer.

## API Endpoint

**POST `/report`**

This endpoint is used by clients to report their system resource usage and status. The client should send both static configuration information and dynamic load metrics in a single request periodically.

### Authentication

Clients must authenticate by providing their secret key in the HTTP headers.

* **Header:** `X-Node-Secret`
* **Value:** The secret key assigned to the node during creation in the Admin Dashboard.

### Request Payload (JSON)

The client should send a JSON payload with the following fields:

| Field | Type | Description |
|-------|------|-------------|
| `os` | String | Operating system or Distribution name (e.g., `Ubuntu 22.04 LTS`, `Windows 11`, `macOS 14.0`). |
| `kernel_version` | String | System kernel version (e.g., `5.15.0-89-generic`). |
| `uptime` | Integer | System uptime in seconds. |
| `cpu` | Float | Current overall CPU usage percentage (e.g., `15.3` for 15.3%). |
| `mem_total` | Integer | Total physical memory in bytes. |
| `mem_used` | Integer | Used physical memory in bytes. |
| `swap_total` | Integer | Total swap space in bytes. |
| `swap_used` | Integer | Used swap space in bytes. |
| `disk_total` | Integer | Total disk space of the main partition (e.g., `/` on Linux, `C:` on Windows) in bytes. |
| `disk_used` | Integer | Used disk space of the main partition in bytes. |
| `net_rx` | Integer | Current network receive rate across all network interfaces in bytes per second. |
| `net_tx` | Integer | Current network transmit rate across all network interfaces in bytes per second. |
| `net_total_rx` | Integer | Total bytes received across all network interfaces since boot. |
| `net_total_tx` | Integer | Total bytes transmitted across all network interfaces since boot. |
| `ip` | String | Main IP address of the system (e.g., `192.168.1.100` or public IP). |
| `ip_stack` | String | Indicates the IP stack supported by the host. Possible values: `"ipv4"`, `"ipv6"`, or `"dual"` (for dual-stack). |

#### Example Request Payload:

```json
{
  "os": "Ubuntu 22.04 LTS",
  "kernel_version": "5.15.0-89-generic",
  "uptime": 86400,
  "cpu": 15.3,
  "mem_total": 4294967296,
  "mem_used": 1073741824,
  "swap_total": 2147483648,
  "swap_used": 536870912,
  "disk_total": 85899345920,
  "disk_used": 21474836480,
  "net_rx": 10240,
  "net_tx": 5120,
  "net_total_rx": 10737418240,
  "net_total_tx": 5368709120,
  "ip": "198.51.100.23",
  "ip_stack": "ipv4"
}
```

### Example Curl Request

```bash
curl -X POST https://your-fastprobe-domain.com/report \
     -H "Content-Type: application/json" \
     -H "X-Node-Secret: YOUR_SECRET_KEY_HERE" \
     -d '{
           "os": "Ubuntu 22.04 LTS",
           "kernel_version": "5.15.0-89-generic",
           "uptime": 3600,
           "cpu": 12.4,
           "mem_total": 4096000000,
           "mem_used": 2048000000,
           "swap_total": 1024000000,
           "swap_used": 0,
           "disk_total": 50000000000,
           "disk_used": 10000000000,
           "net_rx": 1000,
           "net_tx": 500,
           "net_total_rx": 5000000,
           "net_total_tx": 2500000,
           "ip": "10.0.0.5",
           "ip_stack": "ipv4"
         }'
```

### Response

The server will respond with an HTTP status code and, upon success, a JSON payload containing configuration instructions for the client.

- **200 OK**: The report was received and processed successfully. The response body contains configuration data.
- **400 Bad Request**: The payload was invalid (e.g., malformed JSON).
- **401 Unauthorized**: The `X-Node-Secret` header was missing or the secret key is invalid.

#### Success Response Payload (JSON)

When the server returns `200 OK`, it sends the following JSON payload instructing the client on its behavior:

| Field | Type | Description |
|-------|------|-------------|
| `report_interval` | Integer | The interval in seconds that the client should wait before sending the next report (e.g., `10` for 10 seconds). |

#### Example Success Response:

```json
{
  "report_interval": 10
}
```

---

## Public / Frontend Endpoints

While most frontend endpoints are internal to the dashboard, the following endpoint is publicly available for fetching node historical data (if the node is configured to show details, or if the requester is logged in as an admin).

### Get Node History

**GET `/api/node/{id}/history?period={period}`**

Fetches the historical snapshot data (CPU, RAM, Network) for a specific node over a specified period, along with the 5 most recent online/offline events.

#### Path Parameters
- `id` (Integer): The ID of the node.

#### Query Parameters
- `period` (String, Optional): The time span for the historical data. Valid values are `30m` (30 minutes), `1d` (1 day), `3d` (3 days), `7d` (7 days). Defaults to `30m` if omitted or invalid.

#### Response

- **200 OK**: Returns a JSON object containing the downsampled historical data and recent events.
- **403 Forbidden**: The node does not exist, or the node details are configured to be hidden from public visitors.

#### Example Response (JSON)

```json
{
  "history": [
    {
      "recorded_at": "2026-06-04T14:00:00Z",
      "cpu": 15.3,
      "mem_used": 1073741824,
      "mem_total": 4294967296,
      "net_rx": 10240,
      "net_tx": 5120,
      "disk_used": 21474836480,
      "disk_total": 85899345920
    }
  ],
  "events": [
    {
      "event": "online",
      "created_at": "2026-06-04T13:50:00Z"
    }
}
```

---

## Admin Endpoints

The following endpoints require admin authentication via the `admin_session` cookie.

### Alert Management

**Alert Channels**
* **GET `/api/admin/alert_channels`**: Returns a list of configured alert channels.
* **POST `/api/admin/alert_channels`**: Creates a new alert channel.
* **PUT `/api/admin/alert_channels/{id}`**: Updates an existing alert channel.
* **DELETE `/api/admin/alert_channels/{id}`**: Deletes an alert channel.
* **POST `/api/admin/alert_channels/test`**: Sends a test alert to the provided channel configuration.

**Alert Rules**
* **GET `/api/admin/alert_rules`**: Returns a list of configured alert rules.
* **POST `/api/admin/alert_rules`**: Creates a new alert rule.
* **PUT `/api/admin/alert_rules/{id}`**: Updates an existing alert rule.
* **DELETE `/api/admin/alert_rules/{id}`**: Deletes an alert rule.

**Alert Logs**
* **GET `/api/admin/alert_logs`**: Returns the most recent 50 alert logs, showing success/failure status of pushed alerts.

#### Example Payload for Creating a Channel (Gotify)

```json
{
  "name": "My Gotify Server",
  "type": "gotify",
  "config": {
    "url": "https://push.example.com",
    "token": "A1b2C3d4E5f6G7h",
    "priority": 5
  }
}
```

#### Example Payload for Creating a Rule (High Load)

```json
{
  "name": "High CPU Alert",
  "enabled": true,
  "nodes": [1, 2],
  "channels": [1],
  "conditions": [
    {
      "type": "high_load",
      "metric": "cpu",
      "threshold": 80.0
    }
  ]
}
```
