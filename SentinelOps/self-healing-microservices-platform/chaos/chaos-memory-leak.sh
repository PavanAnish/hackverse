#!/bin/bash
curl -X POST "http://localhost:8003/chaos/fail?enable=true"
curl -s http://localhost:8003/health > /dev/null &
