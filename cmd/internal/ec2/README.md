### Amazon EC2/ Linux Server

### Introduction

- The Postman Insights Agent runs as a systemd service on your server
- The Postman Insights project is populated with endpoints observed from the traffic arriving at your service.

### Prerequisites

- Your server's OS supports `systemd`
- `root` user

### Usage

- Log in as root user, or use `sudo su` to enable root before running the below command
```
POSTMAN_API_KEY=<postman-api-key> postman-insights-agent setup --collection <postman-collectionID>
```

To check the status or logs please use

```
journalctl -fu postman-insights-agent
```

#### Why is root required?

- To enable and configure the agent as a systemd services
- Env Configuration file location `/etc/default/postman-insights-agent`
- Systemd service file location `/usr/lib/systemd/system/postman-insights-agent.service`

### Uninstall

- You can disable the systemd service using

`sudo systemctl disable --now postman-insights-agent`
