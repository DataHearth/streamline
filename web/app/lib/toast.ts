import { toast as sonnerToast } from "svelte-sonner";

type ToastOpts = Parameters<typeof sonnerToast.success>[1];

export const toast = {
	ok: (msg: string, opts?: ToastOpts) => sonnerToast.success(msg, opts),
	err: (msg: string, opts?: ToastOpts) => sonnerToast.error(msg, opts),
	warn: (msg: string, opts?: ToastOpts) => sonnerToast.warning(msg, opts),
	info: (msg: string, opts?: ToastOpts) => sonnerToast.info(msg, opts),
	promise: sonnerToast.promise,
};
