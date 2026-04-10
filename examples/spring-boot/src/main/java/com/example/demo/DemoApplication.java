package com.example.demo;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.web.bind.annotation.*;
import org.springframework.http.ResponseEntity;
import java.util.Map;

@SpringBootApplication
@RestController
public class DemoApplication {

    public static void main(String[] args) {
        SpringApplication.run(DemoApplication.class, args);
    }

    @GetMapping("/health")
    public Map<String, String> health() {
        return Map.of("status", "ok");
    }

    @GetMapping("/")
    public Map<String, String> index() {
        return Map.of("message", "Hello from Spring Boot behind VibeWarden!");
    }

    @GetMapping("/public")
    public Map<String, String> publicEndpoint() {
        return Map.of("data", "This is public data");
    }

    @GetMapping("/protected")
    public Map<String, String> protectedEndpoint(
            @RequestHeader(value = "X-User-Id", required = false) String userId) {
        return Map.of(
            "user", userId != null ? userId : "anonymous",
            "data", "This is protected data"
        );
    }

    @GetMapping("/headers")
    public ResponseEntity<Map<String, String>> headers(
            @RequestHeader Map<String, String> allHeaders) {
        return ResponseEntity.ok(allHeaders);
    }
}
