#!/bin/bash
# Usage: ./restart.sh <session_name> <server_bin> <config_file> <log_file>

if [ "$#" -ne 4 ]; then
    echo "Error: Requires exactly 4 arguments."
    echo "Usage: $0 <session_name> <server_bin> <config_file> <log_file>"
    exit 1
fi

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

"$DIR/stop.sh" "$1"
sleep 2
"$DIR/start.sh" "$1" "$2" "$3" "$4"

echo "Restart sequence complete."
