# Usage

Get your token from [here](https://access.redhat.com/management/api) and export it as `RH_OFFLINE_TOKEN`.

Then run this binary with these options:

- `-export <file>` to save the subscriptions in a json file
- `-import-url <url>` to load the subscriptions from a remote json file
- `-import-username <user>` to use basic auth for `-import-url`
- `-import-password <pass>` to use basic auth for `-import-url`

## Overwrites

You can also overwrite other settings with these vars:

- `RH_TOKEN_URL`: https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/token"
- `RH_API_URL`: https://api.access.redhat.com/management/v1/subscriptions
- `RH_FETCH_INTERVAL`: 30

You can overwrite the commandline flags with these vars:

- `RH_EXPORT_FILE` overwrites `-export`
- `RH_IMPORT_URL` overwrites `-import-url`
- `RH_IMPORT_USERNAME` overwrites `-import-username`
- `RH_IMPORT_PASSWORD` overwrites `-import-password`
