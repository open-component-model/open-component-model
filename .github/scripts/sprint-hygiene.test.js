import assert from "assert";
import { findCurrentSprint } from "./sprint-hygiene.js";

// ----------------------------------------------------------
// findCurrentSprint tests
// ----------------------------------------------------------
console.log("Testing findCurrentSprint...");

// Returns the sprint that contains today
{
  const iterations = [
    { id: "s1", title: "Sprint 1", startDate: "2026-04-07", duration: 14 },
    { id: "s2", title: "Sprint 2", startDate: "2026-04-21", duration: 14 },
    { id: "s3", title: "Sprint 3", startDate: "2026-05-05", duration: 14 },
  ];
  const result = findCurrentSprint(iterations, "2026-04-25");
  assert.strictEqual(result.id, "s2", "should pick Sprint 2 which contains today");
}

// Returns the most recently started sprint when today is on the start date
{
  const iterations = [
    { id: "s1", title: "Sprint 1", startDate: "2026-04-07", duration: 14 },
    { id: "s2", title: "Sprint 2", startDate: "2026-04-21", duration: 14 },
  ];
  const result = findCurrentSprint(iterations, "2026-04-21");
  assert.strictEqual(result.id, "s2", "should pick sprint starting today");
}

// Falls back to next upcoming sprint when today is before all iterations
{
  const iterations = [
    { id: "s1", title: "Sprint 1", startDate: "2026-05-01", duration: 14 },
    { id: "s2", title: "Sprint 2", startDate: "2026-05-15", duration: 14 },
  ];
  const result = findCurrentSprint(iterations, "2026-04-25");
  assert.strictEqual(result.id, "s1", "should fall back to nearest upcoming sprint");
}

// Returns null when no iterations exist
{
  const result = findCurrentSprint([], "2026-04-25");
  assert.strictEqual(result, null, "should return null for empty iterations");
}

// Handles unsorted input correctly
{
  const iterations = [
    { id: "s3", title: "Sprint 3", startDate: "2026-05-05", duration: 14 },
    { id: "s1", title: "Sprint 1", startDate: "2026-04-07", duration: 14 },
    { id: "s2", title: "Sprint 2", startDate: "2026-04-21", duration: 14 },
  ];
  const result = findCurrentSprint(iterations, "2026-04-25");
  assert.strictEqual(result.id, "s2", "should handle unsorted iterations");
}

// When today is after all iterations, returns the last one
{
  const iterations = [
    { id: "s1", title: "Sprint 1", startDate: "2026-03-01", duration: 14 },
    { id: "s2", title: "Sprint 2", startDate: "2026-03-15", duration: 14 },
  ];
  const result = findCurrentSprint(iterations, "2026-04-25");
  assert.strictEqual(result.id, "s2", "should return the most recently started sprint");
}

console.log("  findCurrentSprint: all passed");

console.log("\nAll sprint-hygiene tests passed.");
