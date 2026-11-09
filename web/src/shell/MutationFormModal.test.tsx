import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Form, Input } from "antd";
import { describe, expect, it, vi } from "vitest";
import { renderAt } from "../test/render";
import { MutationFormModal, type MutationState } from "./MutationFormModal";

function view(mutation: MutationState, onSubmit = vi.fn(), onClose = vi.fn()) {
  renderAt(
    <MutationFormModal<{ name: string }>
      open
      name="probe"
      title="New thing"
      submitText="Create thing"
      mutation={mutation}
      errorText={(e) => `failed: ${(e as Error).message}`}
      onSubmit={onSubmit}
      onClose={onClose}
    >
      <Form.Item label="Name" name="name" rules={[{ required: true, message: "Enter a name" }]}>
        <Input />
      </Form.Item>
    </MutationFormModal>,
  );
  return { onSubmit, onClose };
}

const idle: MutationState = { isPending: false, isError: false, error: null, reset: vi.fn() };

describe("MutationFormModal", () => {
  it("validates, then hands the values and a close callback to onSubmit", async () => {
    const { onSubmit } = view(idle);
    await userEvent.click(screen.getByRole("button", { name: "Create thing" }));
    expect(await screen.findByText("Enter a name")).toBeInTheDocument();
    expect(onSubmit).not.toHaveBeenCalled();
    await userEvent.type(screen.getByLabelText("Name"), "x");
    await userEvent.click(screen.getByRole("button", { name: "Create thing" }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    expect(onSubmit.mock.calls[0]?.[0]).toEqual({ name: "x" });
    expect(typeof onSubmit.mock.calls[0]?.[1]).toBe("function");
  });

  it("shows the error text and resets the mutation when cancelled", async () => {
    const reset = vi.fn();
    const { onClose } = view({ ...idle, isError: true, error: new Error("nope"), reset });
    expect(await screen.findByText("failed: nope")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(reset).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it("disables nothing but shows a loading submit while pending", () => {
    view({ ...idle, isPending: true });
    expect(screen.getByRole("button", { name: /Create thing/ })).toHaveClass("ant-btn-loading");
  });
});
