# 빌드/실행

## Maven
```xml
<dependencyManagement>
  <dependencies>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-dependencies</artifactId>
      <version>3.3.0</version>
      <type>pom</type>
      <scope>import</scope>
    </dependency>
  </dependencies>
</dependencyManagement>
```

## Gradle
```kts
dependencies {
  implementation("org.springframework.boot:spring-boot-starter-web")
  testImplementation("org.springframework.boot:spring-boot-starter-test")
}
```

## Dockerfile(간단)
```dockerfile
FROM eclipse-temurin:21-jre
COPY build/libs/app.jar /app.jar
ENTRYPOINT ["java","-jar","/app.jar"]
```

