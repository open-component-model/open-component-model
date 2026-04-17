<!-- markdownlint-disable MD041 -->
#### What this PR does / why we need it

#### Which issue(s) this PR fixes
<!--
Usage: `Fixes #<issue number>`, or `Fixes (paste link of issue)`.
-->

#### Testing

##### How to test the changes

<!--
Required files to test the changes:

.ocmconfig
```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    repositories:
      - repository:
          type: DockerConfig/v1
          dockerConfigFile: "~/.docker/config.json"
```

Commands that test the change:

```bash
ocm get cv xxx

ocm transfer xxx
```
-->

##### Verification

- [ ] I have added/updated tests for my changes (see [Test Requirements](../CONTRIBUTING.md#test-requirements))
- [ ] Tests pass locally (`task test` and `task test/integration` if applicable)
- [ ] If touching multiple modules, `go work` is enabled (see `go.work`)
- [ ] My changes do not decrease test coverage
- [ ] I have tested the changes locally by running `ocm`
