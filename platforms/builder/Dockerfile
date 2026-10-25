ARG GO_BASE_IMAGE=docker.io/library/golang:1.24.0-alpine3.21@sha256:2d40d4fc278dad38be0777d5e2a88a2c6dee51b0b29c97a764fc6c6a11ca893c
FROM ${GO_BASE_IMAGE}

ARG BASH_VERSION
ARG BUILD_BASE_VERSION
ARG CA_CERTIFICATES_VERSION
ARG CMAKE_VERSION
ARG E2FSPROGS_VERSION
ARG E2FSPROGS_EXTRA_VERSION
ARG FILE_VERSION
ARG GIT_VERSION
ARG LINUX_HEADERS_VERSION
ARG PATCH_VERSION
ARG PY3_FLATBUFFERS_VERSION
ARG PY3_PIP_VERSION
ARG PYTHON3_VERSION
ARG SAMURAI_VERSION
ARG TINYALSA_DEV_VERSION

RUN apk add --no-cache \
      "bash=${BASH_VERSION}" \
      "build-base=${BUILD_BASE_VERSION}" \
      "ca-certificates=${CA_CERTIFICATES_VERSION}" \
      "cmake=${CMAKE_VERSION}" \
      "e2fsprogs=${E2FSPROGS_VERSION}" \
      "e2fsprogs-extra=${E2FSPROGS_EXTRA_VERSION}" \
      "file=${FILE_VERSION}" \
      "git=${GIT_VERSION}" \
      "linux-headers=${LINUX_HEADERS_VERSION}" \
      "patch=${PATCH_VERSION}" \
      "py3-flatbuffers=${PY3_FLATBUFFERS_VERSION}" \
      "py3-pip=${PY3_PIP_VERSION}" \
      "python3=${PYTHON3_VERSION}" \
      "samurai=${SAMURAI_VERSION}" \
      "tinyalsa-dev=${TINYALSA_DEV_VERSION}" \
    && test "$(go env GOVERSION)" = "go1.24.0" \
    && test -x /usr/bin/ninja

ARG TECHO5_REPOSITORY
ARG TECHO5_ATHENA_COMMIT
COPY freeze-athena-inputs.sh /usr/local/sbin/freeze-athena-inputs
COPY offline-apk /opt/tater/bin/apk.static
RUN chmod 0755 /usr/local/sbin/freeze-athena-inputs /opt/tater/bin/apk.static \
    && freeze-athena-inputs "$TECHO5_REPOSITORY" "$TECHO5_ATHENA_COMMIT" \
    && rm -f /usr/local/sbin/freeze-athena-inputs

ENV GOTOOLCHAIN=local
WORKDIR /src
