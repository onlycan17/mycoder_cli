# 테스트

## 단위/통합 테스트
```java
@SpringBootTest
@AutoConfigureMockMvc
class UserApiTest {
  @Autowired MockMvc mvc;

  @Test
  void hello() throws Exception {
    mvc.perform(get("/api/v1/hello"))
       .andExpect(status().isOk())
       .andExpect(jsonPath("$.message").value("hello"));
  }
}
```

- WebMvcTest: Controller 슬라이스 테스트
- Testcontainers: 실데이터베이스 통합 테스트(PostgreSQL 등)

