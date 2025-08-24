# 오류 처리/검증

- 글로벌 예외 핸들러: `@RestControllerAdvice`
- 검증: Bean Validation(`jakarta.validation`), DTO에 `@NotNull`, `@Email` 등 적용

```java
@RestControllerAdvice
public class GlobalExceptionHandler {
  @ExceptionHandler(MethodArgumentNotValidException.class)
  ResponseEntity<Map<String,Object>> validation(MethodArgumentNotValidException ex) {
    var body = Map.of("error","validation_error","message", ex.getMessage());
    return ResponseEntity.badRequest().body(body);
  }
}
```

