# 보안(Spring Security 기초)

- 의존성: `spring-boot-starter-security`
- 기본: 모든 요청 인증 필요 → 개발환경에서 허용 규칙 구성 필요

```java
@Configuration
@EnableWebSecurity
public class SecurityConfig {
  @Bean SecurityFilterChain filterChain(HttpSecurity http) throws Exception {
    http.csrf(csrf -> csrf.disable())
        .authorizeHttpRequests(auth -> auth
          .requestMatchers("/actuator/**", "/health", "/api/v1/hello").permitAll()
          .anyRequest().authenticated())
        .httpBasic(Customizer.withDefaults());
    return http.build();
  }
}
```

