# FastProbe

FastProbe is a lightweight, self-hosted VPS status probe and monitoring solution (similar to ServerStatus) optimized for shared web hosting environments like **Netcup Web Hosting**.

It features a Go-based backend that receives reports from your VPS nodes via API, directly serves the embedded HTML frontend, and uses AJAX to poll the status. It stores configurations and historical data in a MySQL database.

## Features

- **No external dependencies:** The HTML/JS frontend is embedded into the Go binary.
- **FastCGI Optimized:** Can run directly on cPanel, Plesk, or any shared web hosting that supports FastCGI (like Netcup).
- **Admin Dashboard:** Add and remove nodes securely, set up "Admin-Only" nodes, and manage Pending registrations gracefully.
- **MySQL Backend:** Safe and reliable storage for node data and historical snapshots.
- **Simple API:** Easy for custom clients or shell scripts to report data.

## Recent Updates

- **Single Page Application (SPA) Routing:** Seamless hash-based routing (`#status`, `#admin`, `#setup`). Refreshing your browser will retain your state and keep you in the correct Admin panel tab.
- **Granular Node Details:** Interactive modals displaying detailed real-time metrics including OS, Kernel Version, IP (and stack type), Swap usage, and discrete network metrics.
- **Admin Omni-Access:** When logged in as an Administrator, all data-masking restrictions (such as `Show Details` and `Show IP`) are bypassed, allowing the Admin to view everything globally via the new `/api/admin/status` endpoint.
- **Advanced Tags & Filtering:** Add multiple, reusable tags to any node. Filter nodes in the status view with a dynamic, multi-select tag system (OR logic).
- **Global Settings Control:** Adjust the Global Report Interval dynamically in the Admin Settings tab, which serves as a fallback for any node without a specific polling requirement.
- **Secure Hot-Swappable Credentials:** Update your Admin Username or Admin Password on the fly. Doing so automatically terminates all existing sessions for security, prompting a fresh login.

## Project Structure

- **Server (FastProbeServer):** The Go application that provides the API and frontend.
- **Client:** A script or application on your VPS that sends metrics to the Server (see `API.md`).

## Requirements

To run the server, you need:
- A Web Hosting account that supports FastCGI (Go) or a VPS.
- A MySQL / MariaDB Database.

---

## Deployment on Netcup Web Hosting

Deploying a Go binary on shared web hosting requires configuring Apache/Nginx to route requests to the binary via FastCGI. Here is how to do it on Netcup or Plesk-based hosting:

### 1. Build the Binary
First, compile the application for Linux:

```bash
GOOS=linux GOARCH=amd64 go build -o index.fcgi
```
*(The binary name `index.fcgi` helps many shared hosts detect and map it automatically).*

### 2. Upload Files to your Web Hosting
Upload the compiled `index.fcgi` binary to your `httpdocs` or `public_html` directory. **Make sure it has execute permissions**:
```bash
chmod +x index.fcgi
```

### 3. Setup
Navigate to your domain in your web browser. You should see the setup screen where you can input your MySQL database credentials and create an Admin account.

## Client API

For information on how to build a client or report data to the server, please read the [API.md](API.md).

## Building from Source

If you want to modify the source code or build it yourself:

```bash
git clone <your-repo>
cd FastProbeServer
go build -o index.fcgi
```

---

<div align="center">
    <a href="https://github.com/luoxufeiyan/FastProbeServer" target="_blank" style="text-decoration: none; color: gray; font-size: small;">
        Powered by FastProbe
    </a>
</div>
