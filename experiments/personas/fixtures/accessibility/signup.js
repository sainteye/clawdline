function submitForm() {
  const form = document.getElementById('signup');
  const email = document.getElementById('email');
  const error = document.getElementById('email-error');

  const valid = /^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email.value);
  error.hidden = valid;
  email.style.borderColor = valid ? '' : '#b00020';
  if (!valid || !document.getElementById('terms').checked) {
    return;
  }
  form.submit();
}
