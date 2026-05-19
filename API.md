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
  "ip": "198.51.100.23"
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
           "ip": "10.0.0.5"
         }'
```

### Response

- **200 OK**: The report was received and processed successfully.
- **400 Bad Request**: The payload was invalid (e.g., malformed JSON).
- **401 Unauthorized**: The `X-Node-Secret` header was missing or the secret key is invalid.
