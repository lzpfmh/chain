import TextField from './TextField/TextField'

class NumberField extends TextField {
  constructor(props) {
    super(props)
    this.state = {type: 'number'}
  }
}

export default NumberField