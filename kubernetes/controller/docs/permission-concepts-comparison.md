# Deployer Permission Concepts Comparison

## Context
With the implementation of k8s ApplySet for deployers ([ocm-project#624](https://github.com/open-component-model/ocm-project/issues/624)), the deployer requires get/list/delete permissions for resources in the cluster. This document compares three approaches to handling these permissions.

## Comparison Matrix

| Aspect | 1. Wildcard Access | 2. User-Provided Role | 3. Dynamic Role Creation |
|--------|-------------------|----------------------|--------------------------|
| **Security** | ⚠️ Low - Grants broad permissions that may exceed actual needs | ✅ High - User controls exact permissions | ✅ High - Minimal permissions based on actual resources |
| **Implementation Complexity** | ✅ Simple - Single RBAC manifest | ✅ Simple - Use existing role | ⚠️ Complex - Requires dynamic role management |
| **User Experience** | ✅ Easy - Works out of the box | ⚠️ Moderate - Requires RBAC knowledge | ✅ Easy - Automatic permission setup |
| **Error Handling** | ✅ No permission errors | ❌ Frequent permission errors if misconfigured | ⚠️ Potential edge cases during role creation |
| **Maintenance Burden** | ✅ Low - One-time setup | ❌ High - Users must update as resources change | ⚠️ Moderate - Controller maintains roles |
| **Audit Trail** | ❌ Difficult - Too broad to audit effectively | ✅ Clear - User-defined permissions are explicit | ✅ Clear - Roles match deployed resources |
| **Compliance** | ❌ May violate least-privilege principle | ✅ Can align with organizational policies | ✅ Aligns with least-privilege principle |
| **Multi-Tenancy Support** | ⚠️ Risky - May allow cross-namespace access | ✅ Good - User can scope appropriately | ✅ Good - Scoped to specific resources |
| **Debugging** | ✅ Easy - No permission issues to debug | ❌ Hard - Users must troubleshoot RBAC | ⚠️ Moderate - Controller logs role operations |
| **Resource Overhead** | ✅ Minimal - One role/binding | ⚠️ Minimal - User-managed | ⚠️ Higher - One role per deployer |
| **Flexibility** | ❌ Fixed - All or nothing | ✅ High - Users can customize | ⚠️ Moderate - Based on resource specs |
| **Upgrade Path** | ✅ Easy - No changes needed | ⚠️ Moderate - Users may need to update | ⚠️ Moderate - Controller handles updates |
| **Testing Complexity** | ✅ Low - Single scenario | ❌ High - Must test various configurations | ⚠️ Moderate - Test role generation logic |
| **Documentation Needs** | ✅ Minimal - Simple setup instructions | ⚠️ Extensive - RBAC guide for users | ⚠️ Moderate - Explain automatic behavior |
| **Best For** | Dev/test environments, quick prototypes | Highly regulated environments, security-conscious orgs | Production environments, ease of use |

## Detailed Analysis

### 1. Wildcard Access

**Pros:**
- Simplest to implement and maintain
- No permission-related errors
- Quick setup for users
- Perfect for development/testing

**Cons:**
- Security risk - grants excessive permissions
- Violates principle of least privilege
- May not be acceptable in regulated industries
- Difficult to audit what the deployer actually does
- Potential for unauthorized access to resources

**Example:**
```yaml
rules:
  - apiGroups: ["*"]
    resources: ["*"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

**Use Cases:**
- Development environments
- Quick prototypes
- Single-tenant clusters with trusted users

---

### 2. User-Provided Role

**Pros:**
- Maximum security control
- Users can align with organizational policies
- Clear separation of concerns
- Flexible for different use cases
- Good for compliance requirements

**Cons:**
- Requires RBAC expertise from users
- Error-prone - misconfiguration leads to failures
- High maintenance - must update as deployments change
- Poor user experience for non-K8s experts
- Each deployer may need different permissions

**Example:**
```yaml
# User creates and references their own role
apiVersion: delivery.ocm.software/v1alpha1
kind: Deployer
metadata:
  name: my-deployer
spec:
  serviceAccountName: my-custom-sa  # With user-defined role
  # ...
```

**Use Cases:**
- Enterprise environments with strict security policies
- Multi-tenant platforms
- Regulated industries (finance, healthcare)
- When fine-grained control is required

---

### 3. Dynamic Role Creation

**Pros:**
- Balances security and usability
- Automatic least-privilege permissions
- Good user experience - "just works"
- Auditable - clear what each deployer can do
- Scalable across many deployers

**Cons:**
- Complex to implement correctly
- Controller needs elevated permissions to create roles
- Edge cases in role lifecycle management
- Performance overhead for role CRUD operations
- Must handle role cleanup on deployer deletion

**Implementation Considerations:**
```yaml
# Controller analyzes resources to deploy
# and creates a role automatically:
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: deployer-{name}-managed
  ownerReferences:
    - apiVersion: delivery.ocm.software/v1alpha1
      kind: Deployer
      name: {deployer-name}
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["services", "configmaps"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

**Technical Challenges:**
- Parsing resource specs to determine required permissions
- Handling CRDs and extension API groups
- Role update logic when deployment changes
- Avoiding race conditions during role creation
- Cleanup on deployer deletion (owner references)

**Use Cases:**
- Production environments
- SaaS platforms
- When ease of use is important
- When security is important but users lack RBAC expertise

## Recommendations

### Short-term (Immediate Fix)
**Approach 1: Wildcard Access** for e2e tests and initial release
- Quick to implement
- Unblocks current work
- Mark as "development/testing only" in documentation
- Add security warning

### Medium-term (Next Minor Release)
**Approach 2: User-Provided Role** as default production approach
- Provide comprehensive documentation and examples
- Offer predefined role templates for common scenarios
- Include validation/pre-flight checks for missing permissions
- Good compromise between security and development effort

### Long-term (Future Enhancement)
**Approach 3: Dynamic Role Creation** as optional feature
- Implement as opt-in behavior
- Provide flag to enable: `spec.autoCreateRole: true`
- Fall back to user-provided role if disabled
- Best of both worlds for different use cases

## Hybrid Approach (Recommended)

Combine approaches for maximum flexibility:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Deployer
metadata:
  name: my-deployer
spec:
  rbac:
    # Option 1: Use existing role (approach 2)
    serviceAccountName: my-custom-sa
    
    # Option 2: Auto-create role (approach 3)
    # autoCreate: true
    # scope: namespace  # or 'cluster'
    
    # Option 3: Wildcard (approach 1) - requires explicit opt-in
    # allowWildcard: true  # Only for dev/test
```

**Benefits:**
- Users choose their security/convenience trade-off
- Defaults to secure option (user-provided)
- Supports all use cases
- Clear migration path

## Decision Criteria

Choose based on:

| Priority | Choose Approach |
|----------|----------------|
| **Security is paramount** | Approach 2 (User-Provided) |
| **Ease of use is paramount** | Approach 3 (Dynamic) |
| **Time to market is critical** | Approach 1 (Wildcard) for initial release |
| **Regulatory compliance required** | Approach 2 (User-Provided) |
| **Internal tooling/trusted environment** | Approach 1 (Wildcard) |
| **SaaS/Multi-tenant platform** | Approach 3 (Dynamic) or Hybrid |

## Implementation Roadmap

### Phase 1 (Now)
- ✅ Implement Approach 1 for e2e tests
- Document security implications
- Add warning labels

### Phase 2 (Next Sprint)
- Implement Approach 2 support
- Create role templates
- Write RBAC documentation
- Add permission validation

### Phase 3 (Future)
- Design dynamic role creation
- Implement role generation logic
- Add cleanup mechanisms
- Support hybrid mode

### Phase 4 (Polish)
- Add permission calculator tool
- Improve error messages
- Create troubleshooting guide
- Add metrics/observability