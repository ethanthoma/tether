#!/usr/bin/env bash
set -euo pipefail

for var in TETHER_IMAP_USER TETHER_IMAP_PASSWORD TETHER_ICS_URL TETHER_DISCORD_TOKEN TETHER_DISCORD_CHANNEL TETHER_DISCORD_PUBLIC_KEY; do
	if [ -z "${!var:-}" ]; then
		echo "missing $var - run via: secretspec run -- ./provision.sh" >&2
		exit 1
	fi
done

llama_key_line=$(ssh atlas sudo grep '^LLAMA_API_KEY=' /var/lib/llama-server.env)

{
	printf "TETHER_IMAP_USER='%s'\n" "$TETHER_IMAP_USER"
	printf "TETHER_IMAP_PASSWORD='%s'\n" "$TETHER_IMAP_PASSWORD"
	printf "TETHER_ICS_URL='%s'\n" "$TETHER_ICS_URL"
	printf "TETHER_DISCORD_TOKEN='%s'\n" "$TETHER_DISCORD_TOKEN"
	printf "TETHER_DISCORD_CHANNEL='%s'\n" "$TETHER_DISCORD_CHANNEL"
	printf "TETHER_DISCORD_PUBLIC_KEY='%s'\n" "$TETHER_DISCORD_PUBLIC_KEY"
	for var in TETHER_GOOGLE_CLIENT_ID TETHER_GOOGLE_CLIENT_SECRET TETHER_GOOGLE_REFRESH_TOKEN; do
		[ -n "${!var:-}" ] && printf "%s='%s'\n" "$var" "${!var}"
	done
	printf '%s\n' "$llama_key_line"
} | ssh atlas "sudo install -m 600 -o ethoma -g users /dev/stdin /var/lib/tether.env"

ssh atlas sudo systemctl restart tether-bot

echo "provisioned /var/lib/tether.env on atlas; bot restarted, next pulse activates tether"
