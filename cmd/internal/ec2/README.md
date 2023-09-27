### Amazon EC2/ Linux Server

### Introduction

- The Postman Live Collection Agent (LCA) runs as a systemd service on your server
- The Postman collection is populated with endpoints observed from the traffic arriving at your service.

### Prerequisites[WIP]

- Your server's OS supports `systemd`
- `root` user

### Usage

```
POSTMAN_API_KEY=<postman-api-key> postman-lc-agent setup --collection <postman-collectionID>
```

To check the status or logs please use
```
journald <add-comand-here>
```

#### Why is root required ?[WIP]

- To edit systemd files
- To Enable capabilities


### Uninstall

- You can disable the systemd service using `systemctl disable <add-command-here>`

### Systemd Configuration
- We write files at `<add-location-here>`
