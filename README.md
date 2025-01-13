 Martin Garrix Bot!

A multipurpose bot created exclusively for Garrixers.

![Martin Garrix Bot](https://cdn.discordapp.com/avatars/799613778052382720/28de7ee4e8cc26956e4bf45ecb730b79.webp?size=256 "Martin Garrix Bot")
CLI Flags:
- `--config-path=your-config-path`: Path to the config file.
- `--sync-commands=true`: Synchronize commands with the discord.

## Usage

1. Click on the `Use this template` button to create a new repository from this template.
2. Clone the repository to your local machine.
3. Open the project in your favorite IDE.
4. Copy the `config.example.toml` file to `config.toml` and fill in the required fields.
5. Run the bot using `go run .`

## Configuration

The configuration file is in TOML format. The format is as follows:

```toml
[log]
# valid levels are "debug", "info", "warn", "error"
level = "info"
# valid formats are "text" and "json"
format = "text"
# whether to add the log source to the log message
add_source = true
# log file name
file = "garrixbot.log"
# max size in megabytes before log rotation
max_size = 500
# max age in days before log rotation
max_age = 30
# max number of log files to keep
max_backups = 3


[bot]
# add guild ids the commands should sync to, leave empty to sync globally
dev_guilds = []
# the bot token
token = "your_token_here"
# youtube api key
yt_api_key = "yt_api_key_here"
# google service json file path
google_service_file = "/path/to/servicefile.json"

[database]
# db host
host = "localhost"
# db user
user= "postgres"
# db password
password = "password"
# db name
name = "garrixbot"
# db port
port = 5432
```