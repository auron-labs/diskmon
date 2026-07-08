# Security Policy

## Supported Versions

Security fixes are handled for the latest release and the current `main` branch.

## Reporting a Vulnerability

Use GitHub private vulnerability reporting if it is enabled for this repository.

If private reporting is not available, open a GitHub issue with a short, non-sensitive request for a private contact path. Do not include exploit details, secrets, webhook URLs, device inventories, or other sensitive host information in a public issue.

## Security Notes

diskmon reads local disk health data and can run with elevated privileges when `smartctl` access requires it. Keep the web UI bound to localhost unless you intentionally protect it behind your own network controls, authentication, and TLS.
