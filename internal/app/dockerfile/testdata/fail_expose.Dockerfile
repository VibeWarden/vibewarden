FROM alpine:latest
EXPOSE 8080
USER nonroot
CMD ["./app"]
