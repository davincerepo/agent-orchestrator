import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { FleetQuitDialog } from "./FleetQuitDialog";

describe("Fleet complete exit dialog", () => {
	it("requires confirmation and blocks dismissal while stopping", async () => {
		let finish!: () => void;
		const action = vi.fn(() => new Promise<void>((resolve) => { finish = resolve; }));
		window.ao!.menu.action = action;
		const change = vi.fn();
		render(<FleetQuitDialog open onOpenChange={change} />);
		expect(action).not.toHaveBeenCalled();
		expect(screen.getByText(/Session history and working directories will be kept/)).toBeInTheDocument();
		await userEvent.click(screen.getByRole("button", { name: "Stop Background Processes and Quit" }));
		expect(action).toHaveBeenCalledExactlyOnceWith("fleet.quit");
		expect(screen.getByRole("status")).toHaveTextContent("Stopping Fleet");
		await userEvent.keyboard("{Escape}");
		expect(change).not.toHaveBeenCalled();
		expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
		await act(async () => finish());
	});
	it("keeps the error visible and permits retry", async () => {
		const action = vi.fn().mockRejectedValueOnce(new Error("worker host did not exit")).mockResolvedValueOnce(undefined);
		window.ao!.menu.action = action;
		render(<FleetQuitDialog open onOpenChange={vi.fn()} />);
		const confirm = screen.getByRole("button", { name: "Stop Background Processes and Quit" });
		await userEvent.click(confirm);
		expect(await screen.findByRole("alert")).toHaveTextContent("worker host did not exit");
		await userEvent.click(confirm);
		await waitFor(() => expect(action).toHaveBeenCalledTimes(2));
		expect(screen.queryByRole("alert")).not.toBeInTheDocument();
	});
});
