# 설정 관리

## `application.yml`
```yaml
server:
  port: 8080
spring:
  profiles:
    active: local

app:
  name: demo
  feature-x: true
```

## 타입 세이프 설정 바인딩
```java
@ConfigurationProperties(prefix = "app")
@Data
public class AppProps {
  private String name;
  private boolean featureX;
}

@Configuration
@EnableConfigurationProperties(AppProps.class)
public class AppConfig { }
```

