FROM alpine:latest
EXPOSE 3000
HEALTHCHECK CMD wget -q -O /dev/null http://localhost:3000/health || exit 1
USER nonroot
CMD ["./app"]
