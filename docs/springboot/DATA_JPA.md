# Spring Data JPA 요약

## Entity/Repository/Service 예시
```java
@Entity @Table(name="users")
public class User {
  @Id @GeneratedValue(strategy=GenerationType.IDENTITY)
  private Long id;
  @Column(nullable=false, unique=true) private String email;
  @Column(nullable=false) private String name;
}

public interface UserRepository extends JpaRepository<User, Long> {
  Optional<User> findByEmail(String email);
}

@Service @RequiredArgsConstructor
public class UserService {
  private final UserRepository userRepository;
  @Transactional(readOnly=true)
  public UserDto get(Long id) { return toDto(userRepository.findById(id).orElseThrow()); }
  @Transactional
  public UserDto create(CreateUserReq req) {
    var u = new User(); u.setEmail(req.getEmail()); u.setName(req.getName());
    return toDto(userRepository.save(u));
  }
}
```

## application.yml (예)
```yaml
spring:
  datasource:
    url: jdbc:postgresql://localhost:5432/app
    username: app
    password: secret
  jpa:
    hibernate:
      ddl-auto: update
    show-sql: true
    properties:
      hibernate.format_sql: true
```

