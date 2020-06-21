FROM gcr.io/distroless/static-debian10:latest

COPY ./majortom.amd64 /majortom
ENTRYPOINT ["/majortom"]
