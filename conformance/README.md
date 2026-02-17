# OCM Conformance Testing

This directory contains conformance tests and reference scenarios that validate OCM's core capabilities and design principles.

## Purpose

Conformance testing ensures that OCM implementations correctly handle:

- Component modeling and construction
- Signing and verification workflows  
- Cross-registry transport and localization
- Air-gap deployment scenarios
- Integration with cloud-native ecosystem tools

## Structure

- `scenarios/` - Reference implementation scenarios that demonstrate end-to-end OCM workflows
- Each scenario includes:
  - Complete working implementation
  - Conformance test suite
  - Documentation and setup instructions
  - CI/CD automation

## Current Scenarios

- [`sovereign-scenario/`](scenarios/sovereign-scenario/) - Demonstrates modeling, signing, transporting, and deploying a multi-service product into an air-gapped sovereign cloud environment (ADR-0013)

## Running Conformance Tests

```bash
# Run all conformance scenarios
task conformance:all

# Run specific scenario
task conformance:sovereign-scenario
```

## Contributing

When adding new conformance scenarios:

1. Create scenario directory under `scenarios/`
2. Include complete working implementation
3. Add conformance test suite in `tests/conformance/`
4. Document setup and validation steps
5. Update this README

Each scenario should validate specific OCM capabilities and serve as a reference implementation for users adopting OCM.