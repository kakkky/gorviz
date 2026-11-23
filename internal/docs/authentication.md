# How to connect to the server (passing the token and the URL)

- Pass the token in the environment variable `GORVIZ_TOKEN`. No operation asks for input
- Pass the server URL in the environment variable `GORVIZ_SERVER_URL` (e.g. `https://gorviz.internal:7070`)
- When the server uses a CA it created itself, pass the SHA-256 of that CA, shown by the server, in `GORVIZ_CA_FINGERPRINT`. For your own CA certificate, pass `GORVIZ_CA_FILE`
- A 401 means the token is missing or revoked. Ask the server's administrator to issue one
