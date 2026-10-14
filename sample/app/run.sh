#!/bin/sh
# Sample app for Docksmith: plain POSIX sh + busybox, no dependencies.
echo "================================================"
echo " ${GREETING:-Hello} from ${APP_NAME:-app}!"
echo "================================================"
cat /app/banner.txt
echo
echo "hostname : $(hostname)"
echo "pid      : $$"
echo "workdir  : $(pwd)"
echo "build    : $(cat /app/BUILD_INFO)"
echo "setup    : $(cat /app/data/setup.txt)"
echo "root dirs: $(ls / | tr '\n' ' ')"
echo "processes:"
ps
