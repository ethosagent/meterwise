import { Alert, Button, Form, Modal } from "antd";
import type { ReactNode } from "react";

/** The slice of a TanStack mutation the modal reads. */
export interface MutationState {
  isPending: boolean;
  isError: boolean;
  error: unknown;
  reset: () => void;
}

interface Props<F> {
  open: boolean;
  onClose: () => void;
  /** Form name, unique on the page (antd derives field ids from it). */
  name: string;
  title: string;
  submitText: string;
  mutation: MutationState;
  /** The readable line for a failed submit. */
  errorText: (error: unknown) => string;
  initialValues?: Record<string, unknown>;
  /** Called with the validated values; call `done` once the mutation succeeded to close and reset. */
  onSubmit: (values: F, done: () => void) => void;
  children: ReactNode;
}

/** A modal around one form and one mutation: Cancel and submit buttons, the error line, reset on close. */
export function MutationFormModal<F>({
  open,
  onClose,
  name,
  title,
  submitText,
  mutation,
  errorText,
  initialValues,
  onSubmit,
  children,
}: Props<F>) {
  const [form] = Form.useForm<F>();
  const close = () => {
    mutation.reset();
    form.resetFields();
    onClose();
  };
  return (
    <Modal
      open={open}
      title={title}
      onCancel={close}
      destroyOnHidden
      footer={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="primary" loading={mutation.isPending} onClick={() => form.submit()}>
            {submitText}
          </Button>
        </>
      }
    >
      <Form
        name={name}
        form={form}
        layout="vertical"
        requiredMark={false}
        {...(initialValues ? { initialValues } : {})}
        onFinish={(values) => onSubmit(values, close)}
      >
        {mutation.isError ? (
          <Alert type="error" showIcon title={errorText(mutation.error)} />
        ) : null}
        {children}
      </Form>
    </Modal>
  );
}
