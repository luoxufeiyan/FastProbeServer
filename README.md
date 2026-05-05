# FastProbe

FastProbe is a lightweight, self-hosted VPS status probe and monitoring solution (similar to ServerStatus) optimized for shared web hosting environments like **Netcup Web Hosting**.

It features a Go-based backend that receives reports from your VPS nodes via API, directly serves the embedded HTML frontend, and uses AJAX to poll the status. It stores configurations and historical data in a MySQL database.

## Features

- **No external dependencies:** The HTML/JS frontend is embedded into the Go binary.
- **FastCGI Optimized:** Can run directly on cPanel, Plesk, or any shared web hosting that supports FastCGI (like Netcup).
- **Admin Dashboard:** Add and remove nodes securely, set up "Admin-Only" nodes.
- **MySQL Backend:** Safe and reliable storage for node data and historical snapshots.
- **Simple API:** Easy for custom clients or shell scripts to report data.

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
GOOS=linux GOARCH=amd64 go build -o FastProbeServer
```

### 2. Upload Files to your Web Hosting
Upload the compiled `FastProbeServer` binary to your `httpdocs` or `public_html` directory. **Make sure it has execute permissions** (e.g., `chmod 755 FastProbeServer`).

### 3. Create the `.fcgi` Wrapper
Web servers like Apache sometimes require a wrapper script with a `.fcgi` extension to recognize it as a FastCGI application.
Create a file named `fastprobe.fcgi` in the same directory:

```bash
#!/bin/bash
# You can set environment variables here if needed
# export FCGI_ADDR="127.0.0.1:9000" # Use this ONLY if you need TCP mode
exec ./FastProbeServer
```
*Note: Make sure `fastprobe.fcgi` has execute permissions (`chmod 755 fastprobe.fcgi`).*

### 4. Create `.htaccess`
Create an `.htaccess` file in your web root directory to route all traffic to the FastCGI wrapper:

```apache
Options +ExecCGI
AddHandler fcgid-script .fcgi

RewriteEngine On
RewriteCond %{REQUEST_FILENAME} !-f
RewriteRule ^(.*)$ fastprobe.fcgi/$1 [QSA,L]
```

### 5. Setup
Navigate to your domain in your web browser. You should see the setup screen where you can input your MySQL database credentials and create an Admin account.

## Client API

For information on how to build a client or report data to the server, please read the [API.md](API.md).

## Building from Source

If you want to modify the source code or build it yourself:

```bash
git clone <your-repo>
cd FastProbe
go build -o FastProbeServer
```
