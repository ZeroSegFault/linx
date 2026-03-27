#!/bin/bash
# Builds lx and installs to /usr/local/bin
set -e
go build -o lx .
sudo install -m 755 lx /usr/local/bin/lx
echo "Linx installed to /usr/local/bin/lx"
