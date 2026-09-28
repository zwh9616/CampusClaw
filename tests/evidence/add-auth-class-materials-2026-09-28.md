# add-auth-class-materials implementation evidence — 2026-09-28

The change was exercised with a disposable Compose project (`campusclaw-apply-check`) and a separate MySQL integration container (`campusclaw_apply_test_db`). The original project volumes were not used by acceptance tests. The temporary Compose containers were stopped, and their two verified test-only volumes were removed. The original `campusclaw-zwh` stack was rebuilt and restored afterward.

| Check | Result | Evidence |
| --- | --- | --- |
| Go vet and full backend/MySQL suite | PASS (`campusclaw/tests` 73.764s) | Run against `campusclaw_apply_test` in disposable MySQL container |
| AC20, DA02, Teacher B class isolation, safe login reasons | PASS | [Verbose MySQL scenarios](../../backend/tests/results-2026-09-28.txt) |
| Teacher B password validation, DEV_PUBLIC_ORIGIN guard, Origin/Referer checks | PASS | [Config and auth scenarios](../../backend/tests/results-config-auth-2026-09-28.txt) |
| Frontend typecheck and production build | PASS | `npm run typecheck`; `npm run build` |
| Nginx syntax | PASS | `nginx -t` using `nginx:1.27-alpine` on the Compose network |
| HTTP acceptance via Nginx, including AC29, AC30, AC31 | 27 checks, 0 failed | [HTTP results](../acceptance/results-2026-09-28.txt) |
| Vite browser acceptance, including AC32, foreign Origin 403, WE06 | 8 checks, 0 failed | [Browser results](../browser/results-vite-2026-09-28.txt) |
| Audit log safety | PASS | Isolated API log: 10 `unknown_account`, 10 `bad_password`; Nginx log: 4 limit events. No tested password or session token value in either log. `password_too_long` shown by the verbose MySQL scenario. |
| Original local stack smoke check | PASS | After rebuild: `teacher_b` login 200; `/api/me` returned `teacher_b`, `teacher`, `Class B`; `/health` returned `ok`. |

The browser run used installed Chrome 153 through `CHROME_PATH`. The older Playwright-cached Chromium on this computer exited shortly after launch; that local browser failure was isolated with a CDP probe before the successful rerun. An initial MySQL integration attempt used credentials from an older test container and failed authentication; the full suite passed after moving to a fresh disposable database.

No real seed password is stored in these evidence files. The local generated Teacher B password is only in the ignored `.env` file.
