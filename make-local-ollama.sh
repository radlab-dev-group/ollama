#!/bin/bash

cmake -B build
cmake --build build

go build -o ollama .

