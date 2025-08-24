# Spring 핵심 애너테이션 요약

- 구성/부트
  - `@SpringBootApplication`: `@Configuration + @EnableAutoConfiguration + @ComponentScan`
  - `@Configuration`, `@Bean`, `@ConfigurationProperties(prefix="app")`
- 웹/컨트롤러
  - `@RestController`, `@Controller`, `@RequestMapping`, `@GetMapping`, `@PostMapping`, `@PathVariable`, `@RequestParam`, `@RequestBody`
  - `@Validated`, `@Valid`, `@ExceptionHandler`, `@ControllerAdvice`
- 데이터 접근
  - `@Entity`, `@Table`, `@Id`, `@GeneratedValue`, `@Column`
  - `@Repository`, `@Transactional`
- 기타
  - `@Service`, `@Component`

