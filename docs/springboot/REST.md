# REST API 패턴

## Controller 예시
```java
@RestController
@RequestMapping("/api/v1/users")
@RequiredArgsConstructor
public class UserController {
  private final UserService userService;

  @GetMapping("/{id}")
  public ResponseEntity<UserDto> get(@PathVariable Long id) {
    return ResponseEntity.ok(userService.get(id));
  }

  @PostMapping
  public ResponseEntity<UserDto> create(@Valid @RequestBody CreateUserReq req) {
    var created = userService.create(req);
    return ResponseEntity.status(HttpStatus.CREATED).body(created);
  }
}
```

## 예외 처리
```java
@RestControllerAdvice
public class ApiExceptionHandler {
  @ExceptionHandler(MethodArgumentNotValidException.class)
  public ResponseEntity<Map<String,Object>> handleValidation(MethodArgumentNotValidException ex) {
    var body = Map.of("error","validation_error","message",ex.getMessage());
    return ResponseEntity.badRequest().body(body);
  }
}
```

