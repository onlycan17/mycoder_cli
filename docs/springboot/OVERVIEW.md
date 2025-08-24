# Spring Boot 개요

- 목적: 스프링 프레임워크 기반 애플리케이션을 신속히 개발·배포할 수 있도록 자동 설정과 스타터 의존성을 제공.
- 핵심 특징:
  - 자동설정(Auto-Configuration), 스타터(`spring-boot-starter-*`), 내장 서버(Tomcat/Jetty/Undertow)
  - 프로덕션 기능: Actuator(헬스/메트릭), 외부 설정(`application.yml`), 로깅, DevTools
- 계층 구조(권장):
  - API/Controller → Service → Repository(JPA/MyBatis) → Domain(Entity/DTO)
  - 설정: `@Configuration`, `@ConfigurationProperties`

## 빠른 시작(예)
```java
// Application
@SpringBootApplication
public class DemoApplication {
  public static void main(String[] args) { SpringApplication.run(DemoApplication.class, args); }
}

// REST Controller
@RestController
@RequestMapping("/api/v1/hello")
public class HelloController {
  @GetMapping public Map<String,String> hello() { return Map.of("message","hello"); }
}
```

