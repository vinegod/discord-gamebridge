#!/bin/bash
if [ "$#" -ne 1 ]; then
    echo "Usage: $0 <session_name>"
    exit 1
fi

SESSION_NAME=$1

if ! tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
    exit 0
fi

# 1. Initiate graceful shutdown
tmux send-keys -t "$SESSION_NAME" "exit" C-m

# 2. Failsafe timeout loop (Maximum 60 seconds)
ATTEMPTS=0
while tmux has-session -t "$SESSION_NAME" 2>/dev/null; do
    sleep 2
    ATTEMPTS=$((ATTEMPTS + 1))

    if [ "$ATTEMPTS" -ge 30 ]; then
        echo "Error: Server failed to exit gracefully. Forcing session termination."
        tmux kill-session -t "$SESSION_NAME"
        exit 1
    fi
done

echo "Server stopped gracefully."
