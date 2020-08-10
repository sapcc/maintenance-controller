FROM golang:1.14-buster
COPY entrypoint.sh /entrypoint.sh
RUN mkdir /kubebuilder && curl -L https://go.kubebuilder.io/dl/2.3.1/linux/amd64 | tar -xz -C /kubebuilder && chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
