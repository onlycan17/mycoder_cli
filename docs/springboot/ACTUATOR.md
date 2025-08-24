# 운영(Actuator)

- 의존성: `spring-boot-starter-actuator`
- 설정 예시:
```yaml
management:
  endpoints:
    web:
      exposure:
        include: "health,info,metrics,threaddump,httpexchanges"
  endpoint:
    health:
      show-details: always
```

- 유용한 엔드포인트: `/actuator/health`, `/actuator/metrics`, `/actuator/httpexchanges`

