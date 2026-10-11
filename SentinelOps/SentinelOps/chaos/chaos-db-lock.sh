#!/bin/bash
curl -X POST "http://localhost:8002/chaos/fail?enable=true"
curl -s http://localhost:8002/pay > /dev/null &
