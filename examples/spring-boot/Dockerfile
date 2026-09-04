# The build stage and the runtime stage must stay on the SAME Java major.
# The pom sets no <java.version>, so javac targets the JDK it runs on: a
# Temurin 26 build stage emits class-file version 70, which the Temurin 25
# runtime below refuses with UnsupportedClassVersionError. Dependabot bumps
# the two images independently and proposed exactly that split in #1499
# (#1505), so the maven image is ignored in .github/dependabot.yml and the
# runtime `eclipse-temurin` bump is the single trigger for moving the pair.
# Bump both lines together, to an LTS major, or set <java.version> first.
# Enforced by TestSpringBootExample_BuildAndRuntimeShareJavaMajor.
FROM maven:3.9-eclipse-temurin-25-alpine AS build
WORKDIR /app
COPY pom.xml .
RUN mvn dependency:go-offline -B
COPY src ./src
RUN mvn package -DskipTests -B

FROM eclipse-temurin:25-jre-alpine
RUN apk add --no-cache wget
WORKDIR /app
COPY --from=build /app/target/*.jar app.jar
EXPOSE 3000
CMD ["java", "-jar", "app.jar"]
