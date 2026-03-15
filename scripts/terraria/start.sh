#!/bin/bash
if [ "$#" -ne 4 ]; then
    echo "Usage: $0 <session_name> <server_bin> <config_file> <log_file>"
    exit 1
fi

SESSION_NAME=$1
SERVER_BIN=$2
CONFIG_FILE=$3
LOG_FILE=$4
LOG_DIR=$(dirname "$LOG_FILE")

if tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
    echo "Server is running in tmux session: $SESSION_NAME"
    exit 1
fi

mkdir -p "$LOG_DIR"
if [ -f "$LOG_FILE" ]; then
    mv "$LOG_FILE" "$LOG_DIR/server_$(date +%Y%m%d_%H%M%S).log"
fi
touch "$LOG_FILE"

# 1. Chain the execution: When SERVER_BIN finishes, immediately execute kill-session
tmux new-session -d -s "$SESSION_NAME" "bash -c '$SERVER_BIN -config $CONFIG_FILE; tmux kill-session -t $SESSION_NAME'"

# 2. Re-apply the pipe-pane logging
tmux pipe-pane -t "$SESSION_NAME" -o "cat >> $LOG_FILE"

echo "Server started in session: $SESSION_NAME"
