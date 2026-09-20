import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { findParameterModel, ServiceTierControl, resolveServiceTier } from "./ModelParametersControl";
const model = { id: "test-model", isDefault: true, serviceTiers: [{ id: "priority", name: "Fast" }] };
describe("Codex service tier", () => {
 it("sends explicit Fast off instead of inheriting priority", async () => {
  const onChange = vi.fn(); const user = userEvent.setup();
  render(<ServiceTierControl model={model} value="priority" onChange={onChange} />);
  await user.click(screen.getByRole("button", { name: "Fast" }));
  await user.click(screen.getByRole("menuitem", { name: "Fast off" }));
  expect(onChange).toHaveBeenLastCalledWith("default");
 });
 it("hides Fast for unsupported and unknown custom models", () => {
  render(<ServiceTierControl model={findParameterModel([model], "custom")} value="priority" onChange={vi.fn()} />);
  expect(screen.queryByRole("button", { name: "Fast" })).not.toBeInTheDocument();
  expect(findParameterModel([model], "")).toBe(model);
  expect(resolveServiceTier({}, "priority")).toBe("default");
 });
});
