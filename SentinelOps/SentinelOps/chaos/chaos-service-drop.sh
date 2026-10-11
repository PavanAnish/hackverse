#!/bin/bash
curl -X POST "http://localhost:8001/chaos/fail?enable=true"
curl -s http://localhost:8001/order > /dev/null &
