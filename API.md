# FastProbe Client API Documentation

FastProbeServer exposes an API endpoint for client nodes to report their status. This document describes how a client script can interact with the FastProbeServer.

## API Endpoint

**POST `/report`**

This endpoint is used by clients to report their system resource usage and status.

### Authentication

Clients must authenticate by providing their secret key in the HTTP headers.

* **Header:** `X-Node-Secret`
* **Value:** The secret key assigned to the node during creation in the Admin Dashboard.

### Request Payload (JSON)

The client should send a JSON payload with the following fields:

| Field | Type | Description |
|-------|------|-------------|
| `cpu` | Float | Current CPU usage percentage (e.g., `12.5` for 12.5%). |
| `mem_used` | Integer | Used memory in bytes. |
| `mem_total` | Integer | Total memory in bytes. |
| `net_rx` | Integer | Current network receive rate in bytes per second. |
| `net_tx` | Integer | Current network transmit rate in bytes per second. |
| `disk_used` | Integer | Used disk space in bytes. |
| `disk_total` | Integer | Total disk space in bytes. |
| `uptime` | Integer | System uptime in seconds. |

#### Example Request Payload:

```json
{
  "cpu": 15.3,
  "mem_used": 1073741824,
  "mem_total": 4294967296,
  "net_rx": 10240,
  "net_tx": 5120,
  "disk_used": 21474836480,
  "disk_total": 85899345920,
  "uptime": 86400
}
```

### Example Curl Request

```bash
curl -X POST https://your-fastprobe-domain.com/report \
     -H "Content-Type: application/json" \
     -H "X-Node-Secret: YOUR_SECRET_KEY_HERE" \
     -d '{
           "cpu": 12.4,
           "mem_used": 2048000,
           "mem_total": 4096000,
           "net_rx": 1000,
           "net_tx": 500,
           "disk_used": 10000000,
           "disk_total": 50000000,
           "uptime": 3600
         }'
```

### Response

- **200 OK**: The report was received and processed successfully.
- **400 Bad Request**: The payload was invalid (e.g., malformed JSON).
- **401 Unauthorized**: The `X-Node-Secret` header was missing or the secret key is invalid.
