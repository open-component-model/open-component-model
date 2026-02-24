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

- [ ] I have tested the changes locally by running `ocm`
  