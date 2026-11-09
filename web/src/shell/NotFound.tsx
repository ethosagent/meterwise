import { Button, Result } from "antd";
import { Link } from "react-router-dom";
import { DEFAULT_PATH } from "./nav";

export function NotFound() {
  return (
    <Result
      status="404"
      title="Page not found"
      subTitle="That address does not match any page."
      extra={
        <Link to={DEFAULT_PATH}>
          <Button type="primary">Back to overview</Button>
        </Link>
      }
    />
  );
}
