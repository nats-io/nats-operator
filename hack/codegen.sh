#!/bin/bash

# Copyright 2017 The nats-operator Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

ROOT_DIR="$(dirname "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)")"
BIN_DIR="${ROOT_DIR}/bin"

DEEPCOPY_GEN_BIN="${BIN_DIR}/deepcopy-gen"
GENGO_PACKAGE="github.com/kubernetes/gengo"

PACKAGE="github.com/nats-io/nats-operator"

function build_deepcopy_gen {
    go build \
        -o "${DEEPCOPY_GEN_BIN}" \
        "${GOPATH}/src/${GENGO_PACKAGE}/examples/deepcopy-gen/main.go"
}

function get_gengo {
    local RESULT
    local OUTPUT

    OUTPUT="$(go get -d -u "${GENGO_PACKAGE}" 2>&1)"
    RESULT=$?

    if [[ ${RESULT} -ne 0 ]]; then
        if [[ ${OUTPUT} != *"no buildable Go source files"* ]]; then
            echo -n "${OUTPUT}"
            exit 1
        fi
    fi
}

function generate_deepcopy_funcs {
    ${DEEPCOPY_GEN_BIN} \
        --v 1 \
        --logtostderr \
        --output-file-base zz_generated.deepcopy \
        --input-dirs "${PACKAGE}/pkg/spec"
}

get_gengo && \
build_deepcopy_gen && \
generate_deepcopy_funcs
